package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/your-org/mcp-observability/internal/grafana"
)

func (s *Server) toolGrafanaListDashboards(ctx context.Context, args map[string]any) (ToolResult, error) {
	query := strArgOpt(args, "query", "")
	folder := strArgOpt(args, "folder", "")
	limitStr := strArgOpt(args, "limit", "50")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}

	dashboards, err := s.graf.ListDashboards(ctx, query, folder, limit)
	if err != nil {
		return ToolResult{}, fmt.Errorf("listing dashboards: %w", err)
	}
	return jsonResult(map[string]any{
		"total":      len(dashboards),
		"dashboards": dashboards,
	})
}

func (s *Server) toolGrafanaGetDashboard(ctx context.Context, args map[string]any) (ToolResult, error) {
	uid, err := strArg(args, "uid")
	if err != nil {
		return ToolResult{}, err
	}

	dashboard, err := s.graf.GetDashboard(ctx, uid)
	if err != nil {
		return ToolResult{}, fmt.Errorf("getting dashboard: %w", err)
	}
	return jsonResult(dashboard)
}

func (s *Server) toolGrafanaCreateDashboard(ctx context.Context, args map[string]any) (ToolResult, error) {
	title, err := strArg(args, "title")
	if err != nil {
		return ToolResult{}, err
	}
	metricsStr, err := strArg(args, "metrics")
	if err != nil {
		return ToolResult{}, err
	}

	folder := strArgOpt(args, "folder", "General")
	tagsStr := strArgOpt(args, "tags", "")

	metrics := splitTrim(metricsStr)
	var tags []string
	if tagsStr != "" {
		tags = splitTrim(tagsStr)
	}

	req := grafana.CreateDashboardRequest{
		Title:   title,
		Metrics: metrics,
		Folder:  folder,
		Tags:    tags,
	}

	result, err := s.graf.CreateDashboard(ctx, req)
	if err != nil {
		return ToolResult{}, fmt.Errorf("creating dashboard: %w", err)
	}
	return jsonResult(result)
}

func (s *Server) toolGrafanaListAlertRules(ctx context.Context, args map[string]any) (ToolResult, error) {
	namespace := strArgOpt(args, "namespace", "")

	rules, err := s.graf.ListAlertRules(ctx, namespace)
	if err != nil {
		return ToolResult{}, fmt.Errorf("listing alert rules: %w", err)
	}
	return jsonResult(map[string]any{
		"rules": rules,
	})
}

func (s *Server) toolGrafanaCreateAlert(ctx context.Context, args map[string]any) (ToolResult, error) {
	name, err := strArg(args, "name")
	if err != nil {
		return ToolResult{}, err
	}
	expr, err := strArg(args, "expr")
	if err != nil {
		return ToolResult{}, err
	}
	summary, err := strArg(args, "summary")
	if err != nil {
		return ToolResult{}, err
	}

	severity := strArgOpt(args, "severity", "warning")
	forDuration := strArgOpt(args, "for_duration", "5m")
	folder := strArgOpt(args, "folder", "Alerts")

	req := grafana.CreateAlertRequest{
		Name:        name,
		Expr:        expr,
		Summary:     summary,
		Severity:    severity,
		ForDuration: forDuration,
		Folder:      folder,
	}

	result, err := s.graf.CreateAlertRule(ctx, req)
	if err != nil {
		return ToolResult{}, fmt.Errorf("creating alert rule: %w", err)
	}
	return jsonResult(result)
}

func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
