package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config holds connection parameters for the Prometheus HTTP API.
type Config struct {
	URL         string
	Username    string
	Password    string
	BearerToken string
}

// Client wraps the Prometheus HTTP API (read-only endpoints only).
type Client struct {
	cfg        Config
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Prometheus client and validates the base URL.
func NewClient(cfg Config) (*Client, error) {
	u, err := url.ParseRequestURI(cfg.URL)
	if err != nil || u.Scheme == "" {
		return nil, fmt.Errorf("invalid PROMETHEUS_URL %q: must be a full URL, e.g. http://prometheus:9090", cfg.URL)
	}
	return &Client{
		cfg:        cfg,
		baseURL:    strings.TrimRight(cfg.URL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ─── API types ───────────────────────────────────────────────────────────────

type apiResponse struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// ─── Public methods ───────────────────────────────────────────────────────────

// Query executes an instant PromQL query.
func (c *Client) Query(ctx context.Context, query, ts string) (any, error) {
	params := url.Values{"query": {query}}
	if ts != "" {
		params.Set("time", ts)
	}
	return c.get(ctx, "/api/v1/query", params)
}

// QueryRange executes a range PromQL query.
func (c *Client) QueryRange(ctx context.Context, query, start, end, step string) (any, error) {
	params := url.Values{
		"query": {query},
		"start": {start},
		"end":   {end},
		"step":  {step},
	}
	return c.get(ctx, "/api/v1/query_range", params)
}

// ListMetrics returns all metric names.
func (c *Client) ListMetrics(ctx context.Context) ([]string, error) {
	raw, err := c.get(ctx, "/api/v1/label/__name__/values", nil)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(raw)
	var result struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		// try as plain []string
		var names []string
		if err2 := json.Unmarshal(b, &names); err2 == nil {
			return names, nil
		}
	}

	// The API response data field is a []string directly
	var names []string
	dataRaw, _ := json.Marshal(raw)
	_ = json.Unmarshal(dataRaw, &names)
	return names, nil
}

// Labels returns label names for a metric (or all labels if metric=="").
func (c *Client) Labels(ctx context.Context, metric, start, end string) ([]string, error) {
	params := url.Values{}
	if metric != "" {
		params.Set("match[]", metric)
	}
	if start != "" {
		params.Set("start", start)
	}
	if end != "" {
		params.Set("end", end)
	}
	raw, err := c.get(ctx, "/api/v1/labels", params)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(raw)
	var labels []string
	_ = json.Unmarshal(b, &labels)
	return labels, nil
}

// LabelValues returns values for a specific label on a metric.
func (c *Client) LabelValues(ctx context.Context, metric, label, start, end string) ([]string, error) {
	params := url.Values{}
	if metric != "" {
		params.Set("match[]", metric)
	}
	if start != "" {
		params.Set("start", start)
	}
	if end != "" {
		params.Set("end", end)
	}
	path := fmt.Sprintf("/api/v1/label/%s/values", url.PathEscape(label))
	raw, err := c.get(ctx, path, params)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(raw)
	var vals []string
	_ = json.Unmarshal(b, &vals)
	return vals, nil
}

// Series returns series matching the given selector.
func (c *Client) Series(ctx context.Context, match, start, end string) ([]map[string]string, error) {
	params := url.Values{"match[]": {match}}
	if start != "" {
		params.Set("start", start)
	}
	if end != "" {
		params.Set("end", end)
	}
	raw, err := c.get(ctx, "/api/v1/series", params)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(raw)
	var series []map[string]string
	_ = json.Unmarshal(b, &series)
	return series, nil
}

// Alerts returns currently active alerts.
func (c *Client) Alerts(ctx context.Context) (any, error) {
	return c.get(ctx, "/api/v1/alerts", nil)
}

// Rules returns alerting and recording rules.
func (c *Client) Rules(ctx context.Context, ruleType string) (any, error) {
	params := url.Values{}
	if ruleType != "" {
		params.Set("type", ruleType)
	}
	return c.get(ctx, "/api/v1/rules", params)
}

// Targets returns scrape target discovery info. state may be "active", "dropped", or "any" (default).
func (c *Client) Targets(ctx context.Context, state string) (any, error) {
	params := url.Values{}
	if state != "" {
		params.Set("state", state)
	}
	return c.get(ctx, "/api/v1/targets", params)
}

// Metadata returns metric metadata (type, help, unit). If metric is set, filters to that name.
func (c *Client) Metadata(ctx context.Context, metric, limit string) (any, error) {
	params := url.Values{}
	if metric != "" {
		params.Set("metric", metric)
	}
	if limit != "" {
		params.Set("limit", limit)
	}
	return c.get(ctx, "/api/v1/metadata", params)
}

// TSDBStatus returns TSDB head stats and top cardinality dimensions.
func (c *Client) TSDBStatus(ctx context.Context) (any, error) {
	return c.get(ctx, "/api/v1/status/tsdb", nil)
}

// ─── HTTP internals ───────────────────────────────────────────────────────────

// get performs an authenticated GET and returns the parsed .data field.
func (c *Client) get(ctx context.Context, path string, params url.Values) (any, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing prometheus response: %w", err)
	}
	if apiResp.Status == "error" {
		return nil, fmt.Errorf("prometheus error [%s]: %s", apiResp.ErrorType, apiResp.Error)
	}

	var data any
	if err := json.Unmarshal(apiResp.Data, &data); err != nil {
		return nil, fmt.Errorf("parsing data field: %w", err)
	}
	return data, nil
}

func (c *Client) setAuth(req *http.Request) {
	if c.cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)
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
