package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the MCP SDK server.
type Server = sdkmcp.Server

// CallToolResult wraps the MCP SDK result.
type CallToolResult = sdkmcp.CallToolResult

// TestClient connects to a Server via in-memory transport for testing.
// Returns a function to call tools and a cleanup function.
func TestClient(ctx context.Context, server *Server) (callTool func(ctx context.Context, name string, args map[string]any) (*CallToolResult, error), cleanup func(), err error) {
	t1, t2 := sdkmcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		return nil, nil, fmt.Errorf("server connect: %w", err)
	}

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("client connect: %w", err)
	}

	call := func(ctx context.Context, name string, args map[string]any) (*CallToolResult, error) {
		return session.CallTool(ctx, &sdkmcp.CallToolParams{
			Name:      name,
			Arguments: args,
		})
	}

	return call, func() { session.Close() }, nil
}
