package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DebugServer creates an MCP server with a single "debug_ping" tool that logs invocations.
func DebugServer() *Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "kevinclaw-debug", Version: "v0.0.1"}, nil)

	s.AddTool(&sdkmcp.Tool{
		Name:        "debug_ping",
		Description: "A debug tool that logs its invocation and echoes back the input. Use this to test MCP connectivity.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"message": {"type": "string", "description": "A message to echo back"}
			}
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Message string `json:"message"`
		}
		json.Unmarshal(req.Params.Arguments, &args)

		slog.Info("mcp: debug_ping invoked", "message", args.Message)

		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{
				Text: fmt.Sprintf("pong: %s", args.Message),
			}},
		}, nil
	})

	return s
}

// ServeHTTP starts an MCP server over streamable HTTP on the given address.
// Returns the listener address and a shutdown function.
func ServeHTTP(ctx context.Context, server *Server, addr string) (string, func(), error) {
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("listen: %w", err)
	}

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	listenAddr := fmt.Sprintf("http://%s/mcp", ln.Addr().String())
	slog.Info("mcp: http server started", "addr", listenAddr)

	shutdown := func() {
		srv.Shutdown(ctx)
		ln.Close()
	}

	return listenAddr, shutdown, nil
}
