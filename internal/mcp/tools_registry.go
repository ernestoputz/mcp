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

		"prometheus_targets": {
			Name:        "prometheus_targets",
			Description: "List Prometheus scrape targets with health, last scrape time and last error. Useful for diagnosing broken collection.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"state": {
						Type:        "string",
						Description: "Filter by target state. Defaults to any.",
						Enum:        []string{"active", "dropped", "any", ""},
					},
				},
			},
		},

		"prometheus_metadata": {
			Name:        "prometheus_metadata",
			Description: "Return metric metadata (type, help text, unit) so the caller can build PromQL with full semantic context.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"metric": {Type: "string", Description: "Optional metric name to filter by. Leave empty for all metrics."},
					"limit":  {Type: "string", Description: "Optional max number of metrics to return."},
				},
			},
		},

		"prometheus_tsdb_status": {
			Name:        "prometheus_tsdb_status",
			Description: "Return TSDB head stats and top cardinality dimensions (top series by label, top label-value counts). Use to investigate cardinality explosions.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
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
			Name: "grafana_create_dashboard",
			Description: "Create a Grafana dashboard with one or more panels. " +
				"Prefer the `panels` argument to give each panel a human-readable title instead of showing the raw PromQL expression. " +
				"`metrics` is kept as a shortcut for quick prototyping and uses the expression itself as the panel title.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"title": {Type: "string", Description: "Dashboard title"},
					"panels": {Type: "string", Description: "JSON array of panel specs. Each item: {\"title\":\"HTTP Requests/s\",\"expr\":\"rate(http_requests_total[5m])\",\"legend\":\"{{instance}}\",\"unit\":\"reqps\",\"description\":\"...\",\"type\":\"timeseries|stat|gauge|bargauge\"}. " +
						"Only title and expr are required. Example: [{\"title\":\"CPU %\",\"expr\":\"avg(rate(node_cpu_seconds_total[5m]))\",\"unit\":\"percentunit\"}]"},
					"metrics": {Type: "string", Description: "(Legacy / quick mode) Comma-separated PromQL expressions; each becomes a panel titled with the expression itself. Ignored if `panels` is provided."},
					"folder":  {Type: "string", Description: "Grafana folder name. Defaults to General."},
					"tags":    {Type: "string", Description: "Comma-separated tags to apply to the dashboard"},
				},
				Required: []string{"title"},
			},
		},

		"grafana_update_dashboard": {
			Name:        "grafana_update_dashboard",
			Description: "Update an existing Grafana dashboard by UID. Use to rename the dashboard, replace its panels (e.g. give them human titles), or change tags. Any field left empty keeps its current value; providing `panels` replaces the panel list entirely.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"uid":    {Type: "string", Description: "Dashboard UID (from grafana_list_dashboards)"},
					"title":  {Type: "string", Description: "New dashboard title. Leave empty to keep the current one."},
					"panels": {Type: "string", Description: "JSON array of panel specs (same format as grafana_create_dashboard). If provided, replaces the existing panels."},
					"tags":   {Type: "string", Description: "Comma-separated tags. Leave empty to keep existing tags."},
				},
				Required: []string{"uid"},
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

		"grafana_list_datasources": {
			Name:        "grafana_list_datasources",
			Description: "List all datasources configured in Grafana (Prometheus, Loki, Tempo, etc.). Returns name, type, UID and URL for each.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},

		"grafana_test_datasource": {
			Name:        "grafana_test_datasource",
			Description: "Probe a Grafana datasource health endpoint by UID. Returns whether the datasource is reachable and any error from the backend.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"uid": {Type: "string", Description: "Datasource UID (from grafana_list_datasources)"},
				},
				Required: []string{"uid"},
			},
		},

		"grafana_query_datasource": {
			Name: "grafana_query_datasource",
			Description: "Execute an ad-hoc query against any Grafana datasource via /api/ds/query. " +
				"Use the appropriate expression for the datasource type (PromQL for Prometheus, LogQL for Loki, etc.).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"datasource_uid": {Type: "string", Description: "Target datasource UID (from grafana_list_datasources)"},
					"query":          {Type: "string", Description: "Query expression to execute (e.g. PromQL, LogQL)"},
					"from":           {Type: "string", Description: "Start time as epoch milliseconds (string). Defaults to 1h ago."},
					"to":             {Type: "string", Description: "End time as epoch milliseconds (string). Defaults to now."},
				},
				Required: []string{"datasource_uid", "query"},
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
