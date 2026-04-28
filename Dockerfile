# ─── Stage 1: build ──────────────────────────────────────────────────────────
# BUILDPLATFORM = host arch (avoids QEMU emulation cost during compile).
# TARGETOS/TARGETARCH = where the binary will run; populated by BuildKit.
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Copy go.mod (no go.sum needed — zero external dependencies)
COPY go.mod ./
RUN go mod download

# Copy source and cross-compile a fully static binary for the target arch.
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -extldflags=-static" \
    -o /bin/mcp-server ./cmd/server

# ─── Stage 2: runtime ────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /bin/mcp-server /mcp-server

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/mcp-server"]
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["/mcp-server", "health"]
