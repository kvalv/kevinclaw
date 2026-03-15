package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kvalv/kevinclaw/internal/cron"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CronServer creates an MCP server with cron scheduling tools.
func CronServer(sched *cron.Scheduler) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "kevinclaw-cron", Version: "v0.0.1"}, nil)

	s.AddTool(&mcp.Tool{
		Name:        "cron_schedule",
		Description: "Schedule a one-off prompt job for the agent to execute.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"session_key": {"type": "string", "description": "Session key to run the prompt in (e.g. channel:thread_ts)"},
				"prompt":      {"type": "string", "description": "The prompt to execute"}
			},
			"required": ["session_key", "prompt"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			SessionKey string `json:"session_key"`
			Prompt     string `json:"prompt"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("invalid arguments: %v", err)}},
				IsError: true,
			}, nil
		}

		if err := sched.Schedule(ctx, cron.PromptJobArgs{
			SessionKey: args.SessionKey,
			Prompt:     args.Prompt,
		}); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("scheduling failed: %v", err)}},
				IsError: true,
			}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Job scheduled."}},
		}, nil
	})

	return s
}
