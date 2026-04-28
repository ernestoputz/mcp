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

## Caddy + Let's Encrypt (Route 53 DNS-01)

For exposing the server publicly with a real (non-self-signed) certificate, run
the included Caddy reverse proxy. It uses the **DNS-01 challenge** so port 80
does **not** need to be reachable — useful when ISPs block 80/443 or when the
firewall only allows non-standard ports like 8443.

### One-time setup

1. Create a Route 53 hosted zone for your domain (or use the existing one).
   Add an `A` record for the subdomain pointing to the host that will run the
   stack.
2. Create an IAM user with the policy below (replace `SUA_ZONE_ID` with the
   hosted-zone ID from Route 53):

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       { "Effect": "Allow",
         "Action": ["route53:ListHostedZonesByName"],
         "Resource": "*" },
       { "Effect": "Allow",
         "Action": [
           "route53:GetChange",
           "route53:ChangeResourceRecordSets",
           "route53:ListResourceRecordSets"
         ],
         "Resource": "arn:aws:route53:::hostedzone/SUA_ZONE_ID" }
     ]
   }
   ```

3. Add to `.env`:

   ```env
   CADDY_DOMAIN=mcpobservability.example.com
   CADDY_EMAIL=you@example.com
   AWS_ACCESS_KEY_ID=AKIA...
   AWS_SECRET_ACCESS_KEY=...
   AWS_REGION=us-east-1
   ```

4. Bring the stack up:

   ```bash
   make up-caddy
   make logs-caddy        # watch the cert issuance
   ```

   First boot takes ~30 s while Caddy provisions the cert. Once you see
   `certificate obtained successfully` in the logs, hit:

   ```bash
   curl https://mcpobservability.example.com:8443/healthz
   ```

### Renewal

Caddy renews the certificate ~30 days before expiry, in the background, using
the same DNS-01 challenge. **No cron, no restart, no manual intervention.**
Certs and the ACME account key live in two named volumes (`caddy_data`,
`caddy_config`) which are listed in `.gitignore`.

### Direct mode vs Caddy mode

| Command | Stack | TLS |
|---------|-------|-----|
| `make up` | only `mcp-observability`, publishes `${HTTP_PORT}` | none, or self-signed via `TLS_CERT_FILE` |
| `make up-caddy` | `mcp-observability` (internal) + `caddy` (publishes 8443) | real Let's Encrypt cert via Route 53 |

Choose by which command you run; the env vars not used by the chosen mode are
silently ignored.

---

## Connecting Claude Desktop / claude.ai (remote HTTPS)

After the server is running publicly with Caddy + Let's Encrypt and OAuth is
configured, connect a remote MCP client like this. The flow is the same in
Claude Desktop and claude.ai — both speak OAuth 2.1 + PKCE.

### 1. Pre-flight checks

```bash
# Verify the discovery document is reachable
curl https://mcpobservability.example.com:8443/.well-known/oauth-protected-resource

# Verify the resource itself returns 401 with WWW-Authenticate
curl -i -X POST https://mcpobservability.example.com:8443/mcp \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
# Expect: HTTP/1.1 401 Unauthorized
#         Www-Authenticate: Bearer realm="mcp", resource_metadata="https://.../.well-known/oauth-protected-resource", error="invalid_token"
```

If either of those fails, fix that first — clients won't be able to connect.

### 2. In claude.ai

1. Open Settings → Connectors → **Add custom connector**.
2. **Server URL**: `https://mcpobservability.example.com:8443/mcp`
3. **Client ID** and **Client Secret**: paste the values you set in
   `OAUTH_CLIENT_ID` and `OAUTH_CLIENT_SECRET`.
4. Save and click **Connect** — claude.ai runs the OAuth flow automatically
   (calls `/authorize`, gets a code, exchanges it at `/token`, stores the
   resulting JWT). On success the connector shows the available tools.

### 3. In Claude Desktop

The custom-connector UI lives under Settings → **Connectors** as well. The
fields are identical. Claude Desktop runs the OAuth flow itself (its own
redirect callback URL — typically a localhost loopback or a custom URI scheme
the app registers — both are accepted by this server).

### 4. Verifying it worked

Tail the server logs while you connect:

```bash
make logs-caddy                            # Caddy access logs
docker logs -f mcp-mcp-observability-1     # MCP server access + OAuth
```

You should see, in order:
1. `GET /.well-known/oauth-protected-resource` → 200
2. `GET /.well-known/oauth-authorization-server` → 200
3. `GET /authorize?...` → 302 (redirect with code)
4. `POST /token` → 200
5. `POST /mcp` with a valid Bearer token → 200

If the client mentions an "invalid redirect_uri" or any 4xx, the request that
caused it will be in the logs and you can adjust `redirect_uri` validation or
investigate from there.

### Important: `OAUTH_ISSUER` must include the port

If you serve on `:8443` (or any non-default port), `OAUTH_ISSUER` **must**
include it:

```env
OAUTH_ISSUER=https://mcpobservability.example.com:8443
```

Omitting the port silently breaks discovery: the metadata documents will
advertise endpoints at `:443`, the JWT `iss` claim will not match what the
client sees, and the connector will fail with confusing 401s.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| Caddy logs `unable to get certificate` | AWS creds missing/wrong, or A record not pointing here yet | Check `AWS_ACCESS_KEY_ID`/`SECRET`/`REGION` in `.env`; verify `dig mcp.example.com` resolves to your IP |
| Client gets 401 with `WWW-Authenticate: Bearer realm="mcp"` | Expected on first call — the client is supposed to follow up with the OAuth flow | Confirm the client supports OAuth 2.1 metadata discovery |
| `invalid redirect_uri` 400 from `/authorize` | Client sent a `redirect_uri` with a blocked scheme (`javascript:`, `data:`, `file:`, `vbscript:`, `blob:`) | Either change the client, or relax the deny list in `internal/oauth/handlers.go` |
| `429 rate limit exceeded` for legitimate users | Several users behind the same NAT/proxy share one bucket | Increase `OAUTH_TOKEN_RATE_PER_MINUTE` / `OAUTH_AUTHORIZE_RATE_PER_MINUTE`, or set `OAUTH_TRUSTED_PROXIES` so each user gets their own bucket |
| `invalid_grant code expired` | Code TTL too short for the round-trip latency | Raise `OAUTH_CODE_TTL` (default `60s`; try `120s`) |
| `invalid_token` after a few minutes of use | Access token TTL hit | Normal — the client should refresh; if it doesn't, check `OAUTH_REFRESH_TTL` and the client's behavior |
| `Permission denied` reading cert files at boot | Cert files owned by host user, container runs as nonroot UID 65532 | `chmod 644 certs/*.crt certs/*.key` (self-signed dev) or `chown 65532:65532 certs/*.key && chmod 600 certs/*.key` |
| `oauth: refusing to start with an http:// issuer` | OAuth configured with HTTP on a non-loopback host | Switch to HTTPS (recommended), or set `OAUTH_ALLOW_INSECURE=true` (dev only) |
| Connector works but `tools/call` fails with empty results | Prometheus/Grafana not actually reachable from the container | Check `PROMETHEUS_URL` / `GRAFANA_URL` are reachable from inside Docker (`host.docker.internal` on Mac, `--network host` on Linux, or in-cluster DNS) |

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
| `OAUTH_ACCESS_TTL` | ⬜ | Access token lifetime (Go duration; default `1h`) |
| `OAUTH_REFRESH_TTL` | ⬜ | Refresh token lifetime (default `720h` = 30d) |
| `OAUTH_CODE_TTL` | ⬜ | Authorization code lifetime (default `60s`) |
| `OAUTH_ALLOW_INSECURE` | ⬜ | Set `true` to allow `http://` issuer on non-loopback (dev only) |
| `OAUTH_TOKEN_RATE_PER_MINUTE` | ⬜ | Per-IP rate limit on `/token` (default `5`) |
| `OAUTH_AUTHORIZE_RATE_PER_MINUTE` | ⬜ | Per-IP rate limit on `/authorize` (default `30`) |
| `OAUTH_FAIL_LIMIT` | ⬜ | Consecutive auth failures before IP block (default `10`) |
| `OAUTH_FAIL_BLOCK_DURATION` | ⬜ | Hard-block duration after fail limit (default `10m`) |
| `OAUTH_TRUSTED_PROXIES` | ⬜ | Comma-separated IPs/CIDRs whose `X-Forwarded-For` is trusted (RFC1918 trusted automatically) |
| `CADDY_DOMAIN` | ⬜§ | Public hostname Caddy serves, e.g. `mcpobservability.example.com` |
| `CADDY_EMAIL` | ⬜§ | Email for Let's Encrypt expiration notices |
| `AWS_ACCESS_KEY_ID` | ⬜§ | AWS creds for the Route 53 DNS-01 challenge |
| `AWS_SECRET_ACCESS_KEY` | ⬜§ | (paired with the above) |
| `AWS_REGION` | ⬜§ | AWS region for the SDK (default `us-east-1`) |

*At least one Grafana auth method is required.
†TLS_CERT_FILE and TLS_KEY_FILE must be set together.
‡All four `OAUTH_*` must be set together; OAuth replaces `MCP_AUTH_TOKEN` when enabled.
§Only consumed when running with `make up-caddy` (Caddy reverse proxy).

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
        "-e", "PROMETHEUS_URL=http://prometheus.example.com:9090",
        "-e", "GRAFANA_URL=http://grafana.example.com:3000",
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
