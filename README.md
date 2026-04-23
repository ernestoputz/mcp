# mcp-observability

MCP server that connects LLMs (Claude, GPT, etc.) to **Prometheus** and **Grafana**.
Written in Go — single static binary, minimal container (~10 MB), Kubernetes-native.

## Features

### Prometheus (read-only, SRE-safe)
| Tool | Description |
|------|-------------|
| `prometheus_query` | Instant PromQL query |
| `prometheus_query_range` | Range PromQL query (trends, graphs) |
| `prometheus_list_metrics` | List available metrics (with filter) |
| `prometheus_labels` | List labels / label values for a metric |
| `prometheus_series` | Find series matching a selector |
| `prometheus_alerts` | Currently firing alerts |
| `prometheus_rules` | Alerting + recording rules (read-only) |

### Grafana (read + structured write)
| Tool | Description |
|------|-------------|
| `grafana_list_dashboards` | List dashboards (with search + folder filter) |
| `grafana_get_dashboard` | Fetch full dashboard JSON by UID |
| `grafana_create_dashboard` | Create a dashboard from PromQL expressions |
| `grafana_list_alert_rules` | List Grafana-managed alert rules |
| `grafana_create_alert` | Create a Grafana alert from a PromQL expression |

**Deliberately excluded (security):** admin APIs, token creation, direct Prometheus write, delete operations.

## Transports

| Mode | Use case | How to enable |
|------|----------|---------------|
| `http` | Claude API, remote clients | `MCP_TRANSPORT=http` (default) |
| `stdio` | Claude Desktop (local) | `MCP_TRANSPORT=stdio` |

---

## Quick Start (Docker Compose)

```bash
# 1. Copy and fill secrets
cp .env.example .env
vim .env

# 2. Start
docker compose up --build

# 3. Test
curl http://localhost:8080/healthz
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $MCP_AUTH_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

---

## Kubernetes Deploy

### Prerequisites
- Kubernetes cluster with `kubectl` configured
- Container registry access
- Prometheus and Grafana already running in-cluster

### Steps

```bash
# 1. Build and push image
make docker-build docker-push REGISTRY=your-registry.example.com

# 2. Update image reference in k8s/deployment.yaml
# Change: image: your-registry/mcp-observability:1.0.0

# 3. Create namespace
kubectl create namespace mcp-observability

# 4. Apply secrets from .env (idempotent — safe to re-run)
make k8s-secret

# 5. Deploy
kubectl apply -f k8s/deployment.yaml

# 6. Verify
kubectl get pods -n mcp-observability
kubectl logs -n mcp-observability -l app.kubernetes.io/name=mcp-observability
```

### Secrets Reference

All credentials are injected via a Kubernetes Secret. The `make k8s-secret` target reads from your `.env` file and applies it idempotently.

| Variable | Required | Description |
|----------|----------|-------------|
| `PROMETHEUS_URL` | ✅ | e.g. `http://prometheus.monitoring:9090` |
| `PROMETHEUS_USERNAME` | ⬜ | Basic auth username |
| `PROMETHEUS_PASSWORD` | ⬜ | Basic auth password |
| `PROMETHEUS_BEARER_TOKEN` | ⬜ | Bearer token (alternative to basic auth) |
| `GRAFANA_URL` | ✅ | e.g. `http://grafana.monitoring:3000` |
| `GRAFANA_API_KEY` | ✅* | Service account token (`glsa_...`) |
| `GRAFANA_USERNAME` | ✅* | Basic auth (if no API key) |
| `GRAFANA_PASSWORD` | ✅* | Basic auth (if no API key) |
| `GRAFANA_ORG_ID` | ⬜ | Defaults to `1` |
| `MCP_AUTH_TOKEN` | ⬜ | Bearer token clients must send to this server |

*At least one Grafana auth method is required.

### Grafana Service Account Setup

1. In Grafana: **Administration → Service Accounts → Add service account**
2. Role: **Editor** (needed to create dashboards and alerts)
3. Click **Add service account token** → copy the `glsa_...` token
4. Set `GRAFANA_API_KEY=glsa_...` in your secret

---

## Claude Desktop (stdio mode)

Add to `~/.config/claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "observability": {
      "command": "/path/to/mcp-server",
      "env": {
        "MCP_TRANSPORT": "stdio",
        "PROMETHEUS_URL": "http://localhost:9090",
        "GRAFANA_URL": "http://localhost:3000",
        "GRAFANA_API_KEY": "glsa_xxx"
      }
    }
  }
}
```

Or with Docker:

```json
{
  "mcpServers": {
    "observability": {
      "command": "docker",
      "args": ["run", "--rm", "-i",
        "--network", "host",
        "-e", "MCP_TRANSPORT=stdio",
        "-e", "PROMETHEUS_URL=http://localhost:9090",
        "-e", "GRAFANA_URL=http://localhost:3000",
        "-e", "GRAFANA_API_KEY=glsa_xxx",
        "mcp-observability:latest"
      ]
    }
  }
}
```

---

## Architecture

```
LLM Client (Claude / API)
        │  JSON-RPC 2.0
        ▼
┌──────────────────────────────────────┐
│         mcp-observability            │
│                                      │
│  Transport layer                     │
│  ├── HTTP + SSE  (remote clients)    │
│  └── stdio       (Claude Desktop)    │
│                                      │
│  MCP dispatcher (tools/list,call)    │
│                                      │
│  Tool handlers                       │
│  ├── Prometheus client  (read-only)  │
│  └── Grafana client     (read+write) │
└──────────────────────────────────────┘
        │                    │
        ▼                    ▼
  Prometheus API       Grafana API
  /api/v1/query        /api/dashboards
  /api/v1/rules        /api/v1/provisioning
```

---

## Development

```bash
# Run locally (HTTP mode)
make run

# Run tests
make test

# Lint
make lint
```

### Adding a new tool

1. Add the tool schema in `internal/mcp/tools_registry.go`
2. Add the handler method in `internal/mcp/tools_prometheus.go` or `tools_grafana.go`
3. Register the handler in the `toolHandlers()` map in `server.go`

Each step is in a different file to make diffs clean and LLM-navigable.

---

## Security Notes

- The server **never writes to Prometheus** — only reads via HTTP API
- Grafana write operations are **scoped**: create dashboards and alerts only
- Admin APIs (user management, token creation, org management) are **not exposed**
- Run as non-root UID 65532 in container
- `readOnlyRootFilesystem: true` in Kubernetes
- All capabilities dropped in the pod spec
