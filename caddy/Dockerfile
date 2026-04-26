# Multi-stage build that produces a Caddy binary with the Route 53 DNS plugin
# baked in. The plugin is needed for the ACME DNS-01 challenge — required when
# port 80 is not reachable from the public internet.
FROM caddy:2-builder AS builder
RUN xcaddy build --with github.com/caddy-dns/route53

FROM caddy:2-alpine
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
