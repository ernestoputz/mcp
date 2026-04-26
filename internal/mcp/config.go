package mcp

import (
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime configuration.
// Every field maps to an environment variable so they can be injected
// via Kubernetes Secrets, Docker secrets, or .env files.
type Config struct {
	// Prometheus
	PrometheusURL      string // PROMETHEUS_URL
	PrometheusUsername string // PROMETHEUS_USERNAME  (basic auth, optional)
	PrometheusPassword string // PROMETHEUS_PASSWORD  (basic auth, optional)
	PrometheusBearerToken string // PROMETHEUS_BEARER_TOKEN (optional)

	// Grafana
	GrafanaURL      string // GRAFANA_URL
	GrafanaAPIKey   string // GRAFANA_API_KEY       (service account token)
	GrafanaUsername string // GRAFANA_USERNAME      (basic auth fallback)
	GrafanaPassword string // GRAFANA_PASSWORD      (basic auth fallback)
	GrafanaOrgID    string // GRAFANA_ORG_ID        (default: 1)

	// HTTP transport
	HTTPHost string // HTTP_HOST (default: 0.0.0.0)
	HTTPPort string // HTTP_PORT (default: 8080)

	// TLS — if both files are set, the HTTP transport serves HTTPS instead of plain HTTP.
	TLSCertFile string // TLS_CERT_FILE (PEM-encoded certificate)
	TLSKeyFile  string // TLS_KEY_FILE  (PEM-encoded private key)

	// Optional auth for the MCP server itself
	MCPAuthToken string // MCP_AUTH_TOKEN (Bearer token clients must send)
}

// LoadConfig reads configuration from environment variables.
// Returns an error if any required variable is missing.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		PrometheusURL:         requireEnv("PROMETHEUS_URL"),
		PrometheusUsername:    os.Getenv("PROMETHEUS_USERNAME"),
		PrometheusPassword:    os.Getenv("PROMETHEUS_PASSWORD"),
		PrometheusBearerToken: os.Getenv("PROMETHEUS_BEARER_TOKEN"),

		GrafanaURL:      requireEnv("GRAFANA_URL"),
		GrafanaAPIKey:   os.Getenv("GRAFANA_API_KEY"),
		GrafanaUsername: os.Getenv("GRAFANA_USERNAME"),
		GrafanaPassword: os.Getenv("GRAFANA_PASSWORD"),
		GrafanaOrgID:    envOrDefault("GRAFANA_ORG_ID", "1"),

		HTTPHost:     envOrDefault("HTTP_HOST", "0.0.0.0"),
		HTTPPort:     envOrDefault("HTTP_PORT", "8080"),
		TLSCertFile:  os.Getenv("TLS_CERT_FILE"),
		TLSKeyFile:   os.Getenv("TLS_KEY_FILE"),
		MCPAuthToken: os.Getenv("MCP_AUTH_TOKEN"),
	}

	var errs []string

	if cfg.PrometheusURL == "" {
		errs = append(errs, "PROMETHEUS_URL is required")
	}
	if cfg.GrafanaURL == "" {
		errs = append(errs, "GRAFANA_URL is required")
	}
	if cfg.GrafanaAPIKey == "" && (cfg.GrafanaUsername == "" || cfg.GrafanaPassword == "") {
		errs = append(errs, "Grafana auth required: set GRAFANA_API_KEY or both GRAFANA_USERNAME+GRAFANA_PASSWORD")
	}
	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		errs = append(errs, "TLS_CERT_FILE and TLS_KEY_FILE must be set together")
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return cfg, nil
}

func requireEnv(key string) string {
	return os.Getenv(key)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
