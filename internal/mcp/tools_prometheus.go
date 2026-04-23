package mcp

import (
	"context"
	"fmt"
	"strings"
)

func (s *Server) toolPrometheusQuery(ctx context.Context, args map[string]any) (ToolResult, error) {
	query, err := strArg(args, "query")
	if err != nil {
		return ToolResult{}, err
	}
	ts := strArgOpt(args, "time", "")

	result, err := s.prom.Query(ctx, query, ts)
	if err != nil {
		return ToolResult{}, fmt.Errorf("prometheus query failed: %w", err)
	}
	return jsonResult(result)
}

func (s *Server) toolPrometheusQueryRange(ctx context.Context, args map[string]any) (ToolResult, error) {
	query, err := strArg(args, "query")
	if err != nil {
		return ToolResult{}, err
	}
	start, err := strArg(args, "start")
	if err != nil {
		return ToolResult{}, err
	}
	end, err := strArg(args, "end")
	if err != nil {
		return ToolResult{}, err
	}
	step := strArgOpt(args, "step", "1m")

	result, err := s.prom.QueryRange(ctx, query, start, end, step)
	if err != nil {
		return ToolResult{}, fmt.Errorf("prometheus range query failed: %w", err)
	}
	return jsonResult(result)
}

func (s *Server) toolPrometheusListMetrics(ctx context.Context, args map[string]any) (ToolResult, error) {
	filter := strArgOpt(args, "filter", "")

	metrics, err := s.prom.ListMetrics(ctx)
	if err != nil {
		return ToolResult{}, fmt.Errorf("listing metrics failed: %w", err)
	}

	if filter != "" {
		filtered := make([]string, 0)
		for _, m := range metrics {
			if strings.Contains(m, filter) {
				filtered = append(filtered, m)
			}
		}
		metrics = filtered
	}

	return jsonResult(map[string]any{
		"total":   len(metrics),
		"metrics": metrics,
	})
}

func (s *Server) toolPrometheusLabels(ctx context.Context, args map[string]any) (ToolResult, error) {
	metric, err := strArg(args, "metric")
	if err != nil {
		return ToolResult{}, err
	}
	labelName := strArgOpt(args, "label_name", "")
	start := strArgOpt(args, "start", "")
	end := strArgOpt(args, "end", "")

	if labelName != "" {
		values, err := s.prom.LabelValues(ctx, metric, labelName, start, end)
		if err != nil {
			return ToolResult{}, fmt.Errorf("getting label values: %w", err)
		}
		return jsonResult(map[string]any{
			"metric":      metric,
			"label":       labelName,
			"values":      values,
		})
	}

	labels, err := s.prom.Labels(ctx, metric, start, end)
	if err != nil {
		return ToolResult{}, fmt.Errorf("getting labels: %w", err)
	}
	return jsonResult(map[string]any{
		"metric": metric,
		"labels": labels,
	})
}

func (s *Server) toolPrometheusSeries(ctx context.Context, args map[string]any) (ToolResult, error) {
	match, err := strArg(args, "match")
	if err != nil {
		return ToolResult{}, err
	}
	start := strArgOpt(args, "start", "")
	end := strArgOpt(args, "end", "")

	series, err := s.prom.Series(ctx, match, start, end)
	if err != nil {
		return ToolResult{}, fmt.Errorf("querying series: %w", err)
	}
	return jsonResult(map[string]any{
		"total":  len(series),
		"series": series,
	})
}

func (s *Server) toolPrometheusAlerts(ctx context.Context, _ map[string]any) (ToolResult, error) {
	alerts, err := s.prom.Alerts(ctx)
	if err != nil {
		return ToolResult{}, fmt.Errorf("fetching alerts: %w", err)
	}
	return jsonResult(alerts)
}

func (s *Server) toolPrometheusRules(ctx context.Context, args map[string]any) (ToolResult, error) {
	ruleType := strArgOpt(args, "type", "")
	rules, err := s.prom.Rules(ctx, ruleType)
	if err != nil {
		return ToolResult{}, fmt.Errorf("fetching rules: %w", err)
	}
	return jsonResult(rules)
}
