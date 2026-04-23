# internal/grafana/client.go
curl -sL https://raw.githubusercontent.com/your-org/mcp-observability/main/internal/grafana/client.go 2>/dev/null || \
cat > internal/grafana/client.go << 'EOF'
package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	URL      string
	APIKey   string
	Username string
	Password string
	OrgID    string
}

type Client struct {
	cfg        Config
	httpClient *http.Client
	baseURL    string
}

func NewClient(cfg Config) (*Client, error) {
	u, err := url.ParseRequestURI(cfg.URL)
	if err != nil || u.Scheme == "" {
		return nil, fmt.Errorf("invalid GRAFANA_URL %q", cfg.URL)
	}
	return &Client{
		cfg:        cfg,
		baseURL:    strings.TrimRight(cfg.URL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

type DashboardSummary struct {
	UID    string `json:"uid"`
	Title  string `json:"title"`
	Folder string `json:"folderTitle"`
	URL    string `json:"url"`
	Tags   []any  `json:"tags"`
}

type DashboardResult struct {
	UID  string `json:"uid"`
	URL  string `json:"url"`
	Meta any    `json:"meta"`
}

type CreateDashboardRequest struct {
	Title   string
	Metrics []string
	Folder  string
	Tags    []string
}

type CreateAlertRequest struct {
	Name        string
	Expr        string
	Summary     string
	Severity    string
	ForDuration string
	Folder      string
}

func (c *Client) ListDashboards(ctx context.Context, query, folder string, limit int) ([]DashboardSummary, error) {
	params := url.Values{"type": {"dash-db"}, "limit": {fmt.Sprintf("%d", limit)}}
	if query != "" {
		params.Set("query", query)
	}
	var dashboards []DashboardSummary
	if err := c.getJSON(ctx, "/api/search", params, &dashboards); err != nil {
		return nil, err
	}
	return dashboards, nil
}

func (c *Client) GetDashboard(ctx context.Context, uid string) (any, error) {
	var result any
	if err := c.getJSON(ctx, "/api/dashboards/uid/"+uid, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CreateDashboard(ctx context.Context, req CreateDashboardRequest) (*DashboardResult, error) {
	panels := make([]map[string]any, 0, len(req.Metrics))
	for i, metric := range req.Metrics {
		panels = append(panels, map[string]any{
			"id": i + 1, "type": "timeseries", "title": metric,
			"gridPos": map[string]int{"x": ((i) % 2) * 12, "y": (i / 2) * 8, "w": 12, "h": 8},
			"targets": []map[string]any{{"expr": metric, "refId": "A", "legendFormat": "{{instance}}"}},
		})
	}
	payload := map[string]any{
		"dashboard": map[string]any{
			"title": req.Title, "tags": req.Tags, "timezone": "browser",
			"schemaVersion": 38, "panels": panels,
			"time": map[string]string{"from": "now-1h", "to": "now"}, "refresh": "30s",
		},
		"overwrite": false, "message": "Created by mcp-observability",
	}
	if req.Folder != "" && req.Folder != "General" {
		if id, err := c.getFolderID(ctx, req.Folder); err == nil && id > 0 {
			payload["folderId"] = id
		}
	}
	var result DashboardResult
	if err := c.postJSON(ctx, "/api/dashboards/db", payload, &result); err != nil {
		return nil, err
	}
	result.URL = c.baseURL + result.URL
	return &result, nil
}

func (c *Client) ListAlertRules(ctx context.Context, namespace string) (any, error) {
	var result any
	if err := c.getJSON(ctx, "/api/v1/provisioning/alert-rules", nil, &result); err != nil {
		return nil, err
	}
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

func (c *Client) CreateAlertRule(ctx context.Context, req CreateAlertRequest) (any, error) {
	datasourceUID, err := c.getPrometheusDatasourceUID(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding prometheus datasource: %w", err)
	}
	folderUID, _ := c.ensureFolderUID(ctx, req.Folder)
	rule := map[string]any{
		"title": req.Name, "condition": "C", "for": req.ForDuration,
		"orgId": 1, "folderUID": folderUID, "ruleGroup": "mcp-managed-alerts",
		"noDataState": "NoData", "execErrState": "Error",
		"labels":      map[string]string{"severity": req.Severity},
		"annotations": map[string]string{"summary": req.Summary, "description": "Alert created via MCP for expression: " + req.Expr},
		"data": []map[string]any{
			{"refId": "A", "datasourceUid": datasourceUID,
				"model":             map[string]any{"expr": req.Expr, "refId": "A", "instant": false, "range": true},
				"relativeTimeRange": map[string]int{"from": 600, "to": 0}},
			{"refId": "B", "datasourceUid": "__expr__",
				"model": map[string]any{"type": "reduce", "refId": "B", "expression": "A", "reducer": "last"}},
			{"refId": "C", "datasourceUid": "__expr__",
				"model": map[string]any{"type": "threshold", "refId": "C", "expression": "B",
					"conditions": []map[string]any{
						{"evaluator": map[string]any{"type": "gt", "params": []float64{0}},
							"operator": map[string]string{"type": "and"},
							"reducer":  map[string]string{"type": "last"}},
					}}},
		},
	}
	var result any
	if err := c.postJSON(ctx, "/api/v1/provisioning/alert-rules", rule, &result); err != nil {
		return nil, err
	}
	return result, nil
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
		if t, _ := ds["type"].(string); t == "prometheus" {
			if uid, _ := ds["uid"].(string); uid != "" {
				return uid, nil
			}
		}
	}
	return "", fmt.Errorf("no prometheus datasource found in Grafana")
}

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
		s := string(bodyBytes)
		if len(s) > 300 {
			s = s[:300] + "..."
		}
		return fmt.Errorf("grafana returned %d: %s", resp.StatusCode, s)
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
EOF


