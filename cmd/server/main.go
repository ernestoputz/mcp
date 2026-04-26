package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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
			oauthSvc, err = oauth.New(oauth.Config{
				Issuer:       cfg.OAuthIssuer,
				ClientID:     cfg.OAuthClientID,
				ClientSecret: cfg.OAuthClientSecret,
				SigningKey:   cfg.OAuthSigningKey,
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
