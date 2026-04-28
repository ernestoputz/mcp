package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/your-org/mcp-observability/internal/mcp"
	"github.com/your-org/mcp-observability/internal/oauth"
	"github.com/your-org/mcp-observability/internal/transport"
)

func main() {
	// Logs must go to stderr — stdout is reserved for the stdio JSON-RPC channel.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slogLevel(),
	}))
	slog.SetDefault(logger)

	cfg, err := mcp.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	server, err := mcp.NewServer(cfg, logger)
	if err != nil {
		slog.Error("failed to create MCP server", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mode := os.Getenv("MCP_TRANSPORT")
	if mode == "" {
		mode = "http" // default
	}

	switch mode {
	case "stdio":
		slog.Info("starting MCP server", "transport", "stdio")
		if err := transport.RunStdio(ctx, server); err != nil {
			slog.Error("stdio transport error", "error", err)
			os.Exit(1)
		}
	case "http":
		addr := fmt.Sprintf("%s:%s", cfg.HTTPHost, cfg.HTTPPort)
		scheme := "http+sse"
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			scheme = "https+sse"
		}

		var oauthSvc *oauth.Service
		if cfg.OAuthClientID != "" {
			accessTTL, err := parseDurationOpt(cfg.OAuthAccessTTL, "OAUTH_ACCESS_TTL")
			if err != nil {
				slog.Error("oauth init failed", "error", err)
				os.Exit(1)
			}
			refreshTTL, err := parseDurationOpt(cfg.OAuthRefreshTTL, "OAUTH_REFRESH_TTL")
			if err != nil {
				slog.Error("oauth init failed", "error", err)
				os.Exit(1)
			}
			codeTTL, err := parseDurationOpt(cfg.OAuthCodeTTL, "OAUTH_CODE_TTL")
			if err != nil {
				slog.Error("oauth init failed", "error", err)
				os.Exit(1)
			}
			tokenRate, err := parseIntOpt(cfg.OAuthTokenRatePerMinute, "OAUTH_TOKEN_RATE_PER_MINUTE")
			if err != nil {
				slog.Error("oauth init failed", "error", err)
				os.Exit(1)
			}
			authzRate, err := parseIntOpt(cfg.OAuthAuthorizeRatePerMinute, "OAUTH_AUTHORIZE_RATE_PER_MINUTE")
			if err != nil {
				slog.Error("oauth init failed", "error", err)
				os.Exit(1)
			}
			failLimit, err := parseIntOpt(cfg.OAuthFailLimit, "OAUTH_FAIL_LIMIT")
			if err != nil {
				slog.Error("oauth init failed", "error", err)
				os.Exit(1)
			}
			failBlock, err := parseDurationOpt(cfg.OAuthFailBlockDuration, "OAUTH_FAIL_BLOCK_DURATION")
			if err != nil {
				slog.Error("oauth init failed", "error", err)
				os.Exit(1)
			}
			var trustedProxies []string
			if cfg.OAuthTrustedProxies != "" {
				for _, p := range strings.Split(cfg.OAuthTrustedProxies, ",") {
					if p = strings.TrimSpace(p); p != "" {
						trustedProxies = append(trustedProxies, p)
					}
				}
			}
			oauthSvc, err = oauth.New(oauth.Config{
				Issuer:                 cfg.OAuthIssuer,
				ClientID:               cfg.OAuthClientID,
				ClientSecret:           cfg.OAuthClientSecret,
				SigningKey:             cfg.OAuthSigningKey,
				AccessTTL:              accessTTL,
				RefreshTTL:             refreshTTL,
				CodeTTL:                codeTTL,
				AllowInsecure:          cfg.OAuthAllowInsecure,
				TokenRatePerMinute:     tokenRate,
				AuthorizeRatePerMinute: authzRate,
				FailLimit:              failLimit,
				FailBlockDuration:      failBlock,
				TrustedProxies:         trustedProxies,
			})
			if err != nil {
				slog.Error("oauth init failed", "error", err)
				os.Exit(1)
			}
		}

		slog.Info("starting MCP server", "transport", scheme, "addr", addr)
		if err := transport.RunHTTP(ctx, server, addr, cfg.TLSCertFile, cfg.TLSKeyFile, oauthSvc); err != nil {
			slog.Error("http transport error", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("unknown transport mode", "mode", mode, "valid", []string{"stdio", "http"})
		os.Exit(1)
	}
}

// parseDurationOpt parses a duration string, returning 0 (oauth defaults) if empty.
func parseDurationOpt(s, name string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, s, err)
	}
	return d, nil
}

// parseIntOpt parses an int, returning 0 (oauth defaults) if empty.
func parseIntOpt(s, name string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, s, err)
	}
	return n, nil
}

func slogLevel() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
