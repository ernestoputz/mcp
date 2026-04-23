package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/your-org/mcp-observability/internal/mcp"
)

// RunHTTP starts the HTTP + SSE transport.
// Endpoints:
//
//	POST /mcp         — JSON-RPC over plain HTTP (stateless, Claude API / remote)
//	GET  /mcp/sse     — SSE session init (MCP over SSE for Claude Desktop)
//	POST /mcp/message — SSE message endpoint
//	GET  /healthz     — liveness probe
//	GET  /readyz      — readiness probe
func RunHTTP(ctx context.Context, srv *mcp.Server, addr string) error {
	h := &httpHandler{srv: srv}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/readyz", handleReadyz)
	mux.Handle("/mcp/sse", h.withMiddleware(http.HandlerFunc(h.handleSSE)))
	mux.Handle("/mcp/message", h.withMiddleware(http.HandlerFunc(h.handleSSEMessage)))
	mux.Handle("/mcp", h.withMiddleware(http.HandlerFunc(h.routeMCP)))

	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("HTTP server listening", "addr", addr)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down HTTP server")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

type httpHandler struct {
	srv *mcp.Server
}

func (h *httpHandler) withMiddleware(next http.Handler) http.Handler {
	return recoverMiddleware(loggingMiddleware(h.authMiddleware(next)))
}

func (h *httpHandler) routeMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handleRPC(w, r)
}

func (h *httpHandler) handleRPC(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, mcp.Response{
			JSONRPC: "2.0",
			Error:   &mcp.RPCError{Code: mcp.ErrCodeParse, Message: "invalid JSON: " + err.Error()},
		})
		return
	}
	resp := h.srv.Handle(r.Context(), raw)
	writeJSON(w, http.StatusOK, resp)
}

func (h *httpHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	msgURL := fmt.Sprintf("%s://%s/mcp/message", scheme, r.Host)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", msgURL)
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (h *httpHandler) handleSSEMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handleRPC(w, r)
}

func (h *httpHandler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := h.srv.AuthToken()
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		bearer, found := strings.CutPrefix(auth, "Bearer ")
		if !found || bearer != token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-observability"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.status,
			"duration", time.Since(start).String(),
			"request_id", fmt.Sprintf("%08x", rand.Uint32()),
		)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "error", rec, "stack", string(debug.Stack()))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleReadyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ready"}`))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
