package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config holds Grafana connection parameters.
type Config struct {
	URL      string
	APIKey   string // Service account token (preferred)
	Username string // Basic auth fallback
	Password string
	OrgID    string
}

// Client wraps the Grafana HTTP API.
type Client struct {
	cfg        Config
	httpClient *http.Client
	baseURL    string
}

// NewClient validates and builds a Grafana client.
func NewClient(cfg Config) (*Client, error) {
	u, err := url.ParseRequestURI(cfg.URL)
	if err != nil || u.Scheme == "" {
		return nil, fmt.Errorf("invalid GRAFANA_URL %q: must be a full URL", cfg.URL)
	}
	return &Client{
		cfg:        cfg,
		baseURL:    strings.TrimRight(cfg.URL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ─── Domain types ─────────────────────────────────────────────────────────────

type DashboardSummary struct {
	UID     string `json:"uid"`
	Title   string `json:"title"`
	Folder  string `json:"folder"`
	URL     string `json:"url"`
	Tags    []any  `json:"tags"`
}

type DashboardResult struct {
	UID  string `json:"uid"`
	URL  string `json:"url"`
	Meta any    `json:"meta"`
}

type CreateDashboardRequest struct {
	Title   string
	Panels  []PanelSpec
	Folder  string
	Tags    []string
}

// PanelSpec describes a single dashboard panel in human terms.
// Only Title and Expr are required; the rest have sensible defaults.
type PanelSpec struct {
	Title       string `json:"title"`
	Expr        string `json:"expr"`
	Legend      string `json:"legend,omitempty"`      // legendFormat, e.g. "{{instance}}"
	Unit        string `json:"unit,omitempty"`        // Grafana unit id, e.g. "short", "percent", "reqps", "bytes"
	Description string `json:"description,omitempty"` // Panel description shown on hover
	Type        string `json:"type,omitempty"`        // Panel type: timeseries (default), stat, gauge, bargauge
}

type UpdateDashboardRequest struct {
	UID    string
	Title  string      // optional, empty = keep existing
	Panels []PanelSpec // optional, empty = keep existing panels
	Tags   []string    // optional, nil = keep existing
}

type CreateAlertRequest struct {
	Name        string
	Expr        string
	Summary     string
	Severity    string
	ForDuration string
	Folder      string
}

// ─── Dashboard methods ────────────────────────────────────────────────────────

// ListDashboards searches Grafana dashboards.
func (c *Client) ListDashboards(ctx context.Context, query, folder string, limit int) ([]DashboardSummary, error) {
	params := url.Values{
		"type":  {"dash-db"},
		"limit": {fmt.Sprintf("%d", limit)},
	}
	if query != "" {
		params.Set("query", query)
	}
	if folder != "" {
		params.Set("folderTitle", folder)
	}

	var dashboards []DashboardSummary
	if err := c.getJSON(ctx, "/api/search", params, &dashboards); err != nil {
		return nil, err
	}
	return dashboards, nil
}

// GetDashboard fetches a dashboard by UID.
func (c *Client) GetDashboard(ctx context.Context, uid string) (any, error) {
	var result any
	if err := c.getJSON(ctx, "/api/dashboards/uid/"+uid, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// CreateDashboard creates a new Grafana dashboard with time-series panels.
func (c *Client) CreateDashboard(ctx context.Context, req CreateDashboardRequest) (*DashboardResult, error) {
	panels := make([]map[string]any, 0, len(req.Panels))
	for i, p := range req.Panels {
		panels = append(panels, buildPanel(i+1, p))
	}

	dashModel := map[string]any{
		"title":         req.Title,
		"tags":          req.Tags,
		"timezone":      "browser",
		"schemaVersion": 38,
		"panels":        panels,
		"time":          map[string]string{"from": "now-1h", "to": "now"},
		"refresh":       "30s",
	}

	payload := map[string]any{
		"dashboard": dashModel,
		"overwrite": false,
		"message":   "Created by mcp-observability",
	}

	if req.Folder != "" && req.Folder != "General" {
		folderID, err := c.getFolderID(ctx, req.Folder)
		if err == nil && folderID > 0 {
			payload["folderId"] = folderID
		}
	}

	var result DashboardResult
	if err := c.postJSON(ctx, "/api/dashboards/db", payload, &result); err != nil {
		return nil, err
	}
	result.URL = c.baseURL + result.URL
	return &result, nil
}

// UpdateDashboard applies partial updates to an existing dashboard: title, panels, tags.
// It fetches the current dashboard, mutates it, and re-saves with overwrite=true.
// Empty fields in req preserve the existing value.
func (c *Client) UpdateDashboard(ctx context.Context, req UpdateDashboardRequest) (*DashboardResult, error) {
	current, err := c.GetDashboard(ctx, req.UID)
	if err != nil {
		return nil, fmt.Errorf("fetching dashboard %s: %w", req.UID, err)
	}
	root, ok := current.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected dashboard response shape")
	}
	dash, ok := root["dashboard"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("dashboard payload missing")
	}

	if req.Title != "" {
		dash["title"] = req.Title
	}
	if req.Tags != nil {
		dash["tags"] = req.Tags
	}
	if len(req.Panels) > 0 {
		panels := make([]map[string]any, 0, len(req.Panels))
		for i, p := range req.Panels {
			panels = append(panels, buildPanel(i+1, p))
		}
		dash["panels"] = panels
	}

	payload := map[string]any{
		"dashboard": dash,
		"overwrite": true,
		"message":   "Updated by mcp-observability",
	}
	if meta, ok := root["meta"].(map[string]any); ok {
		if folderID, ok := meta["folderId"].(float64); ok && folderID > 0 {
			payload["folderId"] = int(folderID)
		}
	}

	var result DashboardResult
	if err := c.postJSON(ctx, "/api/dashboards/db", payload, &result); err != nil {
		return nil, err
	}
	result.URL = c.baseURL + result.URL
	return &result, nil
}

// ─── Alert rule methods ───────────────────────────────────────────────────────

// ListAlertRules returns Grafana-managed alert rules.
func (c *Client) ListAlertRules(ctx context.Context, namespace string) (any, error) {
	path := "/api/v1/provisioning/alert-rules"
	var result any
	if err := c.getJSON(ctx, path, nil, &result); err != nil {
		return nil, err
	}
	// Filter by namespace if specified
	if namespace != "" {
		if rules, ok := result.([]any); ok {
			var filtered []any
			for _, r := range rules {
				if rm, ok := r.(map[string]any); ok {
					if ns, _ := rm["namespace"].(string); ns == namespace {
						filtered = append(filtered, r)
					}
				}
			}
			return filtered, nil
		}
	}
	return result, nil
}

// CreateAlertRule creates a Grafana alert rule backed by a Prometheus datasource.
func (c *Client) CreateAlertRule(ctx context.Context, req CreateAlertRequest) (any, error) {
	datasourceUID, err := c.getPrometheusDatasourceUID(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding prometheus datasource: %w", err)
	}

	rule := map[string]any{
		"title":        req.Name,
		"condition":    "C",
		"for":          req.ForDuration,
		"orgId":        1,
		"folderUID":    "", // will be set below
		"ruleGroup":    "mcp-managed-alerts",
		"noDataState":  "NoData",
		"execErrState": "Error",
		"labels": map[string]string{
			"severity": req.Severity,
		},
		"annotations": map[string]string{
			"summary":     req.Summary,
			"description": fmt.Sprintf("Alert created via MCP for expression: %s", req.Expr),
		},
		"data": []map[string]any{
			{
				"refId":         "A",
				"datasourceUid": datasourceUID,
				"model": map[string]any{
					"expr":    req.Expr,
					"refId":   "A",
					"instant": false,
					"range":   true,
				},
				"queryType":          "",
				"relativeTimeRange": map[string]int{"from": 600, "to": 0},
			},
			{
				"refId":         "B",
				"datasourceUid": "__expr__",
				"model": map[string]any{
					"type":       "reduce",
					"refId":      "B",
					"expression": "A",
					"reducer":    "last",
				},
			},
			{
				"refId":         "C",
				"datasourceUid": "__expr__",
				"model": map[string]any{
					"type":       "threshold",
					"refId":      "C",
					"expression": "B",
					"conditions": []map[string]any{
						{"evaluator": map[string]any{"type": "gt", "params": []float64{0}},
							"operator": map[string]string{"type": "and"},
							"reducer":  map[string]string{"type": "last"}},
					},
				},
			},
		},
	}

	// Resolve or create folder
	folderUID, err := c.ensureFolderUID(ctx, req.Folder)
	if err == nil {
		rule["folderUID"] = folderUID
	}

	var result any
	if err := c.postJSON(ctx, "/api/v1/provisioning/alert-rules", rule, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ─── Datasource methods ───────────────────────────────────────────────────────

// ListDatasources returns all configured datasources (Prometheus, Loki, Tempo, etc.).
func (c *Client) ListDatasources(ctx context.Context) (any, error) {
	var result any
	if err := c.getJSON(ctx, "/api/datasources", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// TestDatasource probes a datasource health endpoint by UID.
func (c *Client) TestDatasource(ctx context.Context, uid string) (any, error) {
	var result any
	if err := c.getJSON(ctx, "/api/datasources/uid/"+url.PathEscape(uid)+"/health", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// QueryDatasource executes an ad-hoc query against a Grafana datasource via /api/ds/query.
// Works for Prometheus (PromQL), Loki (LogQL), etc. — the expression is passed in `expr`.
// from/to accept epoch millis as strings; if empty, defaults to the last 1h window.
func (c *Client) QueryDatasource(ctx context.Context, datasourceUID, query, from, to string) (any, error) {
	if from == "" {
		from = strconv.FormatInt(time.Now().Add(-1*time.Hour).UnixMilli(), 10)
	}
	if to == "" {
		to = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	payload := map[string]any{
		"from": from,
		"to":   to,
		"queries": []map[string]any{
			{
				"refId":         "A",
				"datasource":    map[string]string{"uid": datasourceUID},
				"expr":          query,
				"intervalMs":    60000,
				"maxDataPoints": 1000,
			},
		},
	}
	var result any
	if err := c.postJSON(ctx, "/api/ds/query", payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func buildPanel(id int, p PanelSpec) map[string]any {
	title := p.Title
	if title == "" {
		title = p.Expr // last-resort fallback
	}
	legend := p.Legend
	if legend == "" {
		legend = "{{instance}}"
	}
	panelType := p.Type
	if panelType == "" {
		panelType = "timeseries"
	}

	fieldConfig := map[string]any{
		"defaults": map[string]any{},
	}
	if p.Unit != "" {
		fieldConfig["defaults"].(map[string]any)["unit"] = p.Unit
	}

	panel := map[string]any{
		"id":    id,
		"type":  panelType,
		"title": title,
		"gridPos": map[string]int{
			"x": ((id - 1) % 2) * 12,
			"y": ((id - 1) / 2) * 8,
			"w": 12,
			"h": 8,
		},
		"targets": []map[string]any{
			{
				"expr":         p.Expr,
				"refId":        "A",
				"legendFormat": legend,
			},
		},
		"options": map[string]any{
			"tooltip": map[string]string{"mode": "single"},
		},
		"fieldConfig": fieldConfig,
	}
	if p.Description != "" {
		panel["description"] = p.Description
	}
	return panel
}

func (c *Client) getFolderID(ctx context.Context, name string) (int, error) {
	var folders []map[string]any
	if err := c.getJSON(ctx, "/api/folders", nil, &folders); err != nil {
		return 0, err
	}
	for _, f := range folders {
		if t, _ := f["title"].(string); t == name {
			if id, ok := f["id"].(float64); ok {
				return int(id), nil
			}
		}
	}
	return 0, fmt.Errorf("folder %q not found", name)
}

func (c *Client) ensureFolderUID(ctx context.Context, name string) (string, error) {
	var folders []map[string]any
	if err := c.getJSON(ctx, "/api/folders", nil, &folders); err != nil {
		return "", err
	}
	for _, f := range folders {
		if t, _ := f["title"].(string); t == name {
			uid, _ := f["uid"].(string)
			return uid, nil
		}
	}
	// Create folder
	var created map[string]any
	if err := c.postJSON(ctx, "/api/folders", map[string]string{"title": name}, &created); err != nil {
		return "", err
	}
	uid, _ := created["uid"].(string)
	return uid, nil
}

func (c *Client) getPrometheusDatasourceUID(ctx context.Context) (string, error) {
	var datasources []map[string]any
	if err := c.getJSON(ctx, "/api/datasources", nil, &datasources); err != nil {
		return "", err
	}
	for _, ds := range datasources {
		dsType, _ := ds["type"].(string)
		if dsType == "prometheus" {
			uid, _ := ds["uid"].(string)
			if uid != "" {
				return uid, nil
			}
		}
	}
	return "", fmt.Errorf("no prometheus datasource found in Grafana — please add one via the Grafana UI first")
}

// ─── HTTP internals ───────────────────────────────────────────────────────────

func (c *Client) getJSON(ctx context.Context, path string, params url.Values, target any) error {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	return c.doJSON(req, target)
}

func (c *Client) postJSON(ctx context.Context, path string, body any, target any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)
	return c.doJSON(req, target)
}

func (c *Client) doJSON(req *http.Request, target any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("grafana returned %d: %s", resp.StatusCode, truncate(string(bodyBytes), 300))
	}

	if target != nil {
		if err := json.Unmarshal(bodyBytes, target); err != nil {
			return fmt.Errorf("parsing grafana response: %w", err)
		}
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	if c.cfg.OrgID != "" {
		req.Header.Set("X-Grafana-Org-Id", c.cfg.OrgID)
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		return
	}
	if c.cfg.Username != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
