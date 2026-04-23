package mcp

// buildToolRegistry returns the complete map of tool definitions exposed by this server.
// Tool names and descriptions are written to be self-explanatory for LLM consumers.
func (s *Server) buildToolRegistry() map[string]Tool {
	return map[string]Tool{
		// ── Prometheus ────────────────────────────────────────────────────────
		"prometheus_query": {
			Name:        "prometheus_query",
			Description: "Execute an instant PromQL query against Prometheus. Returns the current value(s) for the given expression.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"query": {Type: "string", Description: "PromQL expression, e.g. up, rate(http_requests_total[5m])"},
					"time":  {Type: "string", Description: "Evaluation timestamp in RFC3339 or Unix timestamp. Defaults to now."},
				},
				Required: []string{"query"},
			},
		},

		"prometheus_query_range": {
			Name:        "prometheus_query_range",
			Description: "Execute a PromQL range query. Useful for graphing metrics over time or detecting trends.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"query": {Type: "string", Description: "PromQL expression"},
					"start": {Type: "string", Description: "Start time in RFC3339 or Unix timestamp"},
					"end":   {Type: "string", Description: "End time in RFC3339 or Unix timestamp"},
					"step":  {Type: "string", Description: "Resolution step, e.g. 15s, 1m, 5m. Defaults to 1m."},
				},
				Required: []string{"query", "start", "end"},
			},
		},

		"prometheus_list_metrics": {
			Name:        "prometheus_list_metrics",
			Description: "List all metric names available in Prometheus. Optionally filter by a prefix or substring.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"filter": {Type: "string", Description: "Optional substring to filter metric names. Leave empty to list all."},
				},
			},
		},

		"prometheus_labels": {
			Name:        "prometheus_labels",
			Description: "Return all label names (or values for a specific label) for a given metric.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"metric":      {Type: "string", Description: "Metric name, e.g. http_requests_total"},
					"label_name":  {Type: "string", Description: "If set, returns values for this label instead of listing all label names."},
					"start":       {Type: "string", Description: "Optional start time for the query range"},
					"end":         {Type: "string", Description: "Optional end time for the query range"},
				},
				Required: []string{"metric"},
			},
		},

		"prometheus_series": {
			Name:        "prometheus_series",
			Description: "Find time series matching a set of label matchers. Useful to understand cardinality and available dimensions.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"match": {Type: "string", Description: `Label matcher selector, e.g. {job="api-server",env="prod"}`},
					"start": {Type: "string", Description: "Optional start time"},
					"end":   {Type: "string", Description: "Optional end time"},
				},
				Required: []string{"match"},
			},
		},

		"prometheus_alerts": {
			Name:        "prometheus_alerts",
			Description: "Return all currently firing alerts from Prometheus Alertmanager integration.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},

		"prometheus_rules": {
			Name:        "prometheus_rules",
			Description: "List all alerting and recording rules loaded in Prometheus. Read-only.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"type": {
						Type:        "string",
						Description: "Filter by rule type",
						Enum:        []string{"alert", "record", ""},
					},
				},
			},
		},

		// ── Grafana ───────────────────────────────────────────────────────────
		"grafana_list_dashboards": {
			Name:        "grafana_list_dashboards",
			Description: "List all dashboards in Grafana. Returns UID, title, folder and URL for each.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"query":  {Type: "string", Description: "Optional search string to filter dashboards by title"},
					"folder": {Type: "string", Description: "Optional folder name to filter results"},
					"limit":  {Type: "string", Description: "Max results to return (default: 50)"},
				},
			},
		},

		"grafana_get_dashboard": {
			Name:        "grafana_get_dashboard",
			Description: "Fetch the full JSON model of a Grafana dashboard by its UID. Useful for inspecting panels, queries and alert rules.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"uid": {Type: "string", Description: "Dashboard UID (from grafana_list_dashboards)"},
				},
				Required: []string{"uid"},
			},
		},

		"grafana_create_dashboard": {
			Name:        "grafana_create_dashboard",
			Description: "Create a new Grafana dashboard with one or more panels backed by Prometheus metrics.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"title":   {Type: "string", Description: "Dashboard title"},
					"metrics": {Type: "string", Description: "Comma-separated list of PromQL expressions for panels, e.g. rate(http_requests_total[5m]),up"},
					"folder":  {Type: "string", Description: "Grafana folder name. Defaults to General."},
					"tags":    {Type: "string", Description: "Comma-separated tags to apply to the dashboard"},
				},
				Required: []string{"title", "metrics"},
			},
		},

		"grafana_list_alert_rules": {
			Name:        "grafana_list_alert_rules",
			Description: "List all Grafana alert rules (Grafana-managed alerts, not Prometheus rules).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"namespace": {Type: "string", Description: "Optional folder/namespace to filter by"},
				},
			},
		},

		"grafana_create_alert": {
			Name:        "grafana_create_alert",
			Description: "Create a Grafana alert rule based on a Prometheus metric expression.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name":         {Type: "string", Description: "Alert rule name"},
					"expr":         {Type: "string", Description: "PromQL expression to evaluate, e.g. rate(http_requests_total[5m]) > 100"},
					"summary":      {Type: "string", Description: "Human-readable summary of what the alert means"},
					"severity":     {Type: "string", Description: "Severity label", Enum: []string{"critical", "warning", "info"}, Default: "warning"},
					"for_duration": {Type: "string", Description: "Duration the condition must be true before firing, e.g. 5m (default: 5m)"},
					"folder":       {Type: "string", Description: "Grafana folder/namespace for the rule (default: Alerts)"},
				},
				Required: []string{"name", "expr", "summary"},
			},
		},
	}
}
