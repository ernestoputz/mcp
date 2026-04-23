package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/your-org/mcp-observability/internal/mcp"
)

// RunStdio runs the MCP server over stdin/stdout (newline-delimited JSON-RPC).
// This is the transport used by Claude Desktop via claude_desktop_config.json.
func RunStdio(ctx context.Context, srv *mcp.Server) error {
	slog.Info("stdio transport started")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1 MB max line

	enc := json.NewEncoder(os.Stdout)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("stdin read error: %w", err)
			}
			slog.Info("stdin closed, shutting down")
			return nil
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		resp := srv.Handle(ctx, line)

		// notifications/initialized returns an empty-ish response — skip writing
		if resp.ID == nil && resp.Result == nil && resp.Error == nil {
			continue
		}

		if err := enc.Encode(resp); err != nil {
			slog.Error("failed to write response", "error", err)
		}
	}
}
