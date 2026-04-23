package mcp

import (
	"context"
	"encoding/json"
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

	panelsStr := strArgOpt(args, "panels", "")
	metricsStr := strArgOpt(args, "metrics", "")

	panels, err := parsePanels(panelsStr, metricsStr)
	if err != nil {
		return ToolResult{}, err
	}
	if len(panels) == 0 {
		return ToolResult{}, fmt.Errorf("must provide at least one panel: set `panels` (preferred) or `metrics`")
	}

	folder := strArgOpt(args, "folder", "General")
	tagsStr := strArgOpt(args, "tags", "")
	var tags []string
	if tagsStr != "" {
		tags = splitTrim(tagsStr)
	}

	req := grafana.CreateDashboardRequest{
		Title:  title,
		Panels: panels,
		Folder: folder,
		Tags:   tags,
	}

	result, err := s.graf.CreateDashboard(ctx, req)
	if err != nil {
		return ToolResult{}, fmt.Errorf("creating dashboard: %w", err)
	}
	return jsonResult(result)
}

func (s *Server) toolGrafanaUpdateDashboard(ctx context.Context, args map[string]any) (ToolResult, error) {
	uid, err := strArg(args, "uid")
	if err != nil {
		return ToolResult{}, err
	}
	title := strArgOpt(args, "title", "")
	panelsStr := strArgOpt(args, "panels", "")
	tagsStr := strArgOpt(args, "tags", "")

	var panels []grafana.PanelSpec
	if panelsStr != "" {
		panels, err = parsePanels(panelsStr, "")
		if err != nil {
			return ToolResult{}, err
		}
	}
	var tags []string
	if tagsStr != "" {
		tags = splitTrim(tagsStr)
	}

	req := grafana.UpdateDashboardRequest{
		UID:    uid,
		Title:  title,
		Panels: panels,
		Tags:   tags,
	}

	result, err := s.graf.UpdateDashboard(ctx, req)
	if err != nil {
		return ToolResult{}, fmt.Errorf("updating dashboard: %w", err)
	}
	return jsonResult(result)
}

// parsePanels prefers the structured `panels` JSON; falls back to the legacy
// comma-separated `metrics` form where the PromQL expression becomes the title.
func parsePanels(panelsJSON, metricsCSV string) ([]grafana.PanelSpec, error) {
	if panelsJSON != "" {
		var specs []grafana.PanelSpec
		if err := json.Unmarshal([]byte(panelsJSON), &specs); err != nil {
			return nil, fmt.Errorf("invalid `panels` JSON: %w", err)
		}
		for i, p := range specs {
			if p.Expr == "" {
				return nil, fmt.Errorf("panel[%d]: `expr` is required", i)
			}
		}
		return specs, nil
	}
	if metricsCSV != "" {
		metrics := splitTrim(metricsCSV)
		out := make([]grafana.PanelSpec, 0, len(metrics))
		for _, m := range metrics {
			out = append(out, grafana.PanelSpec{Title: m, Expr: m})
		}
		return out, nil
	}
	return nil, nil
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
