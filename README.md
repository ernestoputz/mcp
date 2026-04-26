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
| `prometheus_targets` | Scrape targets with health, last scrape, last error |
| `prometheus_metadata` | Metric metadata (type, help, unit) |
| `prometheus_tsdb_status` | TSDB head stats + top cardinality dimensions |

### Grafana (read + structured write)
| Tool | Description |
|------|-------------|
| `grafana_list_dashboards` | List dashboards (with search + folder filter) |
| `grafana_get_dashboard` | Fetch full dashboard JSON by UID |
| `grafana_create_dashboard` | Create a dashboard with human-titled panels (structured JSON) |
| `grafana_update_dashboard` | Update an existing dashboard (rename, replace panels, retag) |
| `grafana_list_alert_rules` | List Grafana-managed alert rules |
| `grafana_create_alert` | Create a Grafana alert from a PromQL expression |
| `grafana_list_datasources` | List configured datasources (Prometheus, Loki, Tempo, …) |
| `grafana_test_datasource` | Probe a datasource health endpoint by UID |
| `grafana_query_datasource` | Ad-hoc query against any datasource via `/api/ds/query` |

### Dashboard authoring — panel titles

Panels can be authored in two ways:

- **`panels`** (preferred) — JSON array of specs, each with a human-readable `title` distinct from the PromQL expression:
  ```json
  [
    {"title":"HTTP Requests/s","expr":"rate(http_requests_total[5m])","legend":"{{handler}}","unit":"reqps"},
    {"title":"CPU %","expr":"avg(rate(node_cpu_seconds_total[5m]))","unit":"percentunit"}
  ]
  ```
  Supported per-panel fields: `title`, `expr` (both required), `legend`, `unit`, `description`, `type` (`timeseries`, `stat`, `gauge`, `bargauge`).

- **`metrics`** (legacy shortcut) — comma-separated PromQL expressions; each becomes a panel titled with the expression itself. Use only for quick prototyping.

Use `grafana_update_dashboard` to rename panels or the dashboard without recreating it.

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

## HTTPS (optional)

The HTTP transport listens with TLS when both `TLS_CERT_FILE` and `TLS_KEY_FILE`
are set. The compose file mounts `./certs` to `/certs:ro` and the healthcheck
adapts to the chosen scheme automatically.

```bash
# 1. Generate a self-signed cert for development
mkdir -p certs
openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
  -keyout certs/server.key -out certs/server.crt \
  -subj "/CN=localhost"
chmod 644 certs/server.key certs/server.crt   # readable by container's nonroot UID

# 2. Add to .env
cat >> .env <<EOF
HTTP_PORT=8443
TLS_CERT_FILE=/certs/server.crt
TLS_KEY_FILE=/certs/server.key
EOF

# 3. Restart
docker compose up -d
curl -k https://localhost:8443/healthz
```

For production, replace the self-signed pair with a real certificate
(Let's Encrypt, internal CA, etc.). The server reloads on container restart.

---

## OAuth 2.0 (required by claude.ai)

The server implements OAuth 2.1 Authorization Code flow with PKCE (RFC 7636).
Access and refresh tokens are stateless JWTs (HS256). When `OAUTH_CLIENT_ID`
is set, OAuth replaces `MCP_AUTH_TOKEN` and the following endpoints become
available:

| Endpoint | Purpose |
|----------|---------|
| `GET /.well-known/oauth-authorization-server` | RFC 8414 metadata |
| `GET /.well-known/oauth-protected-resource` | RFC 9728 metadata |
| `GET /authorize` | Authorization Code grant (PKCE S256 only) |
| `POST /token` | `authorization_code` + `refresh_token` grants |

```bash
# 1. Generate the four secrets
openssl rand -hex 32   # → OAUTH_CLIENT_ID
openssl rand -hex 32   # → OAUTH_CLIENT_SECRET
openssl rand -hex 32   # → OAUTH_SIGNING_KEY

# 2. Add to .env (OAUTH_ISSUER must match the public URL clients use)
cat >> .env <<EOF
OAUTH_ISSUER=https://your-public-mcp-url
OAUTH_CLIENT_ID=...
OAUTH_CLIENT_SECRET=...
OAUTH_SIGNING_KEY=...
EOF

# 3. Restart
docker compose up -d
```

In claude.ai's custom-connector form, paste the **Client ID** and **Client
Secret** along with the server URL. claude.ai discovers `/authorize` and
`/token` via the well-known metadata and runs the PKCE flow automatically.

---

## Multi-arch builds (Raspberry Pi, AWS Graviton, …)

The Dockerfile honors BuildKit's `TARGETOS` / `TARGETARCH`, so building on the
target host produces a binary for the right architecture. For cross-builds
from a different host, use the dedicated make targets:

```bash
make docker-build           # builds for the host architecture
make docker-build-amd64     # explicit linux/amd64
make docker-build-arm64     # explicit linux/arm64 (Raspberry Pi 4/5 64-bit, Apple Silicon, Graviton)
make docker-build-multi     # multi-arch manifest, pushed to $(REGISTRY)
```

Verify the binary inside an image:

```bash
CID=$(docker create mcp-observability:arm64) && \
docker cp "$CID:/mcp-server" /tmp/mcp && docker rm "$CID" && file /tmp/mcp
# /tmp/mcp: ELF 64-bit LSB executable, ARM aarch64, ...
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
| `MCP_AUTH_TOKEN` | ⬜ | Static Bearer token clients must send (use OAuth for remote clients like claude.ai) |
| `HTTP_PORT` | ⬜ | Listen port (default `8080`) |
| `HTTP_HOST` | ⬜ | Listen address (default `0.0.0.0`) |
| `LOG_LEVEL` | ⬜ | One of `debug`, `info`, `warn`, `error` (default `info`) |
| `TLS_CERT_FILE` | ⬜† | PEM cert path (inside the container) — enables HTTPS |
| `TLS_KEY_FILE` | ⬜† | PEM key path (inside the container) — enables HTTPS |
| `OAUTH_ISSUER` | ⬜‡ | Public URL clients reach this server at, e.g. `https://mcp.example.com` |
| `OAUTH_CLIENT_ID` | ⬜‡ | Pre-shared OAuth client id (paste into claude.ai) |
| `OAUTH_CLIENT_SECRET` | ⬜‡ | Pre-shared OAuth client secret (paste into claude.ai) |
| `OAUTH_SIGNING_KEY` | ⬜‡ | HMAC secret used to sign access/refresh JWTs |

*At least one Grafana auth method is required.
†TLS_CERT_FILE and TLS_KEY_FILE must be set together.
‡All four `OAUTH_*` must be set together; OAuth replaces `MCP_AUTH_TOKEN` when enabled.

### Grafana Service Account Setup

1. In Grafana: **Administration → Service Accounts → Add service account**
2. Role: **Editor** (needed to create dashboards and alerts)
3. Click **Add service account token** → copy the `glsa_...` token
4. Set `GRAFANA_API_KEY=glsa_...` in your secret

---

## Claude Desktop (stdio mode)

Config file location (platform-specific):

| OS | Path |
|----|------|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Linux | `~/.config/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |


```json
{
  "mcpServers": {
    "observability": {
      "command": "/usr/local/bin/docker",
      "args": [
        "run",
        "--rm",
        "-i",
        "-e", "MCP_TRANSPORT=stdio",
        "-e", "PROMETHEUS_URL=http:///",
        "-e", "GRAFANA_URL=http://",
        "-e", "GRAFANA_API_KEY=glsa_key_yourgrafanakeyhere",
        "-e", "GRAFANA_ORG_ID=1",
        "-e", "LOG_LEVEL=info",
        "mcp-observability:local"
      ]
    }
  },
  "preferences": {
    "coworkScheduledTasksEnabled": true,
    "ccdScheduledTasksEnabled": true,
    "sidebarMode": "chat",
    "coworkWebSearchEnabled": true
  }
}
```



### Native binary

```json
{
  "mcpServers": {
    "observability": {
      "command": "/absolute/path/to/bin/mcp-server",
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

### Docker

Use the absolute path to `docker` (Claude Desktop does not inherit your shell `PATH` — find yours with `which docker`):

```json
{
  "mcpServers": {
    "observability": {
      "command": "/usr/local/bin/docker",
      "args": [
        "run", "--rm", "-i",
        "-e", "MCP_TRANSPORT=stdio",
        "-e", "PROMETHEUS_URL=http://prometheus.example:9090/",
        "-e", "GRAFANA_URL=http://grafana.example:3000/",
        "-e", "GRAFANA_API_KEY=glsa_xxx",
        "mcp-observability:local"
      ]
    }
  }
}
```

After editing the config, fully quit and reopen Claude Desktop — it only reads the file on startup.

> Notes
> - In stdio mode, stdout is reserved for JSON-RPC; all server logs go to stderr.
> - When targeting Prometheus/Grafana on `127.0.0.1` of the host (not a LAN IP), add `--network host` (Linux) or replace the URL host with `host.docker.internal` (macOS/Windows).

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
- Grafana write operations are **scoped**: create/update dashboards and create alerts only (no delete)
- Admin APIs (user management, token creation, org management) are **not exposed**
- Run as non-root UID 65532 in container
- `readOnlyRootFilesystem: true` in Kubernetes
- All capabilities dropped in the pod spec
