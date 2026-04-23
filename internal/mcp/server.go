package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/your-org/mcp-observability/internal/grafana"
	"github.com/your-org/mcp-observability/internal/prometheus"
)

const (
	serverName    = "mcp-observability"
	serverVersion = "1.0.0"
	mcpProtoVer   = "2024-11-05"
)

// Server is the MCP server. It routes JSON-RPC requests to tool handlers.
type Server struct {
	cfg    *Config
	logger *slog.Logger
	prom   *prometheus.Client
	graf   *grafana.Client
	tools  map[string]Tool
}

// NewServer constructs and validates a ready-to-use Server.
func NewServer(cfg *Config, logger *slog.Logger) (*Server, error) {
	promClient, err := prometheus.NewClient(prometheus.Config{
		URL:         cfg.PrometheusURL,
		Username:    cfg.PrometheusUsername,
		Password:    cfg.PrometheusPassword,
		BearerToken: cfg.PrometheusBearerToken,
	})
	if err != nil {
		return nil, fmt.Errorf("prometheus client: %w", err)
	}

	grafClient, err := grafana.NewClient(grafana.Config{
		URL:      cfg.GrafanaURL,
		APIKey:   cfg.GrafanaAPIKey,
		Username: cfg.GrafanaUsername,
		Password: cfg.GrafanaPassword,
		OrgID:    cfg.GrafanaOrgID,
	})
	if err != nil {
		return nil, fmt.Errorf("grafana client: %w", err)
	}

	s := &Server{
		cfg:    cfg,
		logger: logger,
		prom:   promClient,
		graf:   grafClient,
	}
	s.tools = s.buildToolRegistry()
	return s, nil
}

// AuthToken returns the configured bearer token for the MCP server itself (may be empty).
func (s *Server) AuthToken() string { return s.cfg.MCPAuthToken }

// Handle processes a single JSON-RPC request and returns a Response.
func (s *Server) Handle(ctx context.Context, raw []byte) Response {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return errResponse(nil, ErrCodeParse, "parse error: "+err.Error())
	}

	s.logger.Debug("rpc request", "method", req.Method, "id", string(req.ID))

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		// no-op — client notification, no response needed but we return empty
		return Response{JSONRPC: "2.0", ID: req.ID}
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return errResponse(req.ID, ErrCodeMethodNotFound, "method not found: "+req.Method)
	}
}

// ─── Method handlers ─────────────────────────────────────────────────────────

func (s *Server) handleInitialize(req Request) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: InitializeResult{
			ProtocolVersion: mcpProtoVer,
			ServerInfo:      ServerInfo{Name: serverName, Version: serverVersion},
			Capabilities:    Caps{Tools: &ToolsCap{ListChanged: false}},
		},
	}
}

func (s *Server) handleToolsList(req Request) Response {
	list := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		list = append(list, t)
	}
	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": list},
	}
}

func (s *Server) handleToolsCall(ctx context.Context, req Request) Response {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, ErrCodeInvalidParams, "invalid params: "+err.Error())
	}

	handler, ok := s.toolHandlers()[params.Name]
	if !ok {
		return errResponse(req.ID, ErrCodeMethodNotFound, "unknown tool: "+params.Name)
	}

	result, err := handler(ctx, params.Arguments)
	if err != nil {
		s.logger.Error("tool execution failed", "tool", params.Name, "error", err)
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolResult{
				IsError: true,
				Content: []ContentBlock{{Type: "text", Text: err.Error()}},
			},
		}
	}

	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// toolHandlers maps tool name → handler func. Handlers return a ToolResult.
func (s *Server) toolHandlers() map[string]func(ctx context.Context, args map[string]any) (ToolResult, error) {
	return map[string]func(ctx context.Context, args map[string]any) (ToolResult, error){
		// ── Prometheus ──────────────────────────────────────────────────────
		"prometheus_query":        s.toolPrometheusQuery,
		"prometheus_query_range":  s.toolPrometheusQueryRange,
		"prometheus_list_metrics": s.toolPrometheusListMetrics,
		"prometheus_labels":       s.toolPrometheusLabels,
		"prometheus_series":       s.toolPrometheusSeries,
		"prometheus_alerts":       s.toolPrometheusAlerts,
		"prometheus_rules":        s.toolPrometheusRules,
		// ── Grafana ─────────────────────────────────────────────────────────
		"grafana_list_dashboards":  s.toolGrafanaListDashboards,
		"grafana_get_dashboard":    s.toolGrafanaGetDashboard,
		"grafana_create_dashboard": s.toolGrafanaCreateDashboard,
		"grafana_update_dashboard": s.toolGrafanaUpdateDashboard,
		"grafana_list_alert_rules": s.toolGrafanaListAlertRules,
		"grafana_create_alert":     s.toolGrafanaCreateAlert,
	}
}

// textResult wraps a formatted string into a ToolResult.
func textResult(text string) ToolResult {
	return ToolResult{Content: []ContentBlock{{Type: "text", Text: text}}}
}

// jsonResult marshals v into a pretty-printed ToolResult.
func jsonResult(v any) (ToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ToolResult{}, fmt.Errorf("marshal result: %w", err)
	}
	return textResult(string(b)), nil
}

// strArg extracts a required string argument.
func strArg(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %s must be a string", key)
	}
	return s, nil
}

// strArgOpt extracts an optional string argument with a default.
func strArgOpt(args map[string]any, key, def string) string {
	v, ok := args[key]
	if !ok {
		return def
	}
	s, _ := v.(string)
	if s == "" {
		return def
	}
	return s
}
