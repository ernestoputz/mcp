# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build        # Compile binary to bin/mcp-server (CGO_ENABLED=0)
make run          # Run locally on :8080 (requires .env file)
make test         # go test ./... -race -count=1
make lint         # golangci-lint
make tidy         # go mod tidy
make docker-build # Build Docker image
make k8s-apply    # Apply k8s/deployment.yaml
```

Copy `.env.example` to `.env` and populate before running locally.

To run a single test:
```bash
go test ./internal/mcp/... -run TestFunctionName -v
```

## Architecture

This is a **Model Context Protocol (MCP) server** written in Go that bridges LLMs (Claude, etc.) to Prometheus and Grafana APIs. It exposes 12 tools (7 Prometheus, 5 Grafana) via JSON-RPC 2.0.

**Transport layer** (`internal/transport/`): Two transports exist — HTTP+SSE for remote clients/Claude API (`http.go`) and newline-delimited JSON over stdio for Claude Desktop (`stdio.go`). Transport is selected at startup via `MCP_TRANSPORT` env var.

**MCP protocol** (`internal/mcp/server.go`): `Server.Handle()` is the single dispatcher for all JSON-RPC messages. It handles `initialize`, `tools/list`, and `tools/call`. Tool handlers are registered in a map built by `toolHandlers()` (lines 147–162 of `server.go`).

**Tool implementations**: `tools_prometheus.go` and `tools_grafana.go` each implement their respective tool handlers. Tools receive `map[string]any` arguments and return `(string, error)`. Adding a new tool requires: (1) add a `Tool` definition to `buildToolRegistry()`, (2) add the handler func, (3) register it in `toolHandlers()`.

**Backend clients** (`internal/prometheus/client.go`, `internal/grafana/client.go`): Thin HTTP clients wrapping Prometheus `/api/v1/*` and Grafana REST APIs. Prometheus client is read-only. Grafana client supports controlled writes (dashboards and alert rules only).

**Config** (`internal/mcp/config.go`): All config comes from environment variables — no config files at runtime. Required: `PROMETHEUS_URL`, `GRAFANA_URL`, and Grafana auth (`GRAFANA_API_KEY` or `GRAFANA_USERNAME`+`GRAFANA_PASSWORD`). Optional auth for the MCP server itself via `MCP_AUTH_TOKEN`.

## Key Design Constraints

- **Zero external dependencies** — stdlib only (`net/http`, `encoding/json`, `log/slog`).
- **No writes to Prometheus** — the Prometheus client only performs GET requests.
- **Stateless** — no persistent state; the server can be horizontally scaled.
- HTTP transport auth middleware expects `Authorization: Bearer <MCP_AUTH_TOKEN>` when `MCP_AUTH_TOKEN` is set.
