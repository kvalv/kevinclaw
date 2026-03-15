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
			return errResult("invalid arguments: %v", err), nil
		}
		if err := sched.Schedule(ctx, cron.PromptJobArgs{
			SessionKey: args.SessionKey,
			Prompt:     args.Prompt,
		}); err != nil {
			return errResult("scheduling failed: %v", err), nil
		}
		return textResult("Job scheduled."), nil
	})

	s.AddTool(&mcp.Tool{
		Name:        "cron_list",
		Description: "List all scheduled/pending/running jobs.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jobs, err := sched.List(ctx)
		if err != nil {
			return errResult("listing failed: %v", err), nil
		}
		out, _ := json.Marshal(jobs)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil
	})

	s.AddTool(&mcp.Tool{
		Name:        "cron_cancel",
		Description: "Cancel a scheduled job by ID.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"job_id": {"type": "number", "description": "The job ID to cancel"}
			},
			"required": ["job_id"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			JobID float64 `json:"job_id"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}
		if err := sched.Cancel(ctx, int64(args.JobID)); err != nil {
			return errResult("cancel failed: %v", err), nil
		}
		return textResult("Job %d cancelled.", int64(args.JobID)), nil
	})

	return s
}

func textResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}

func errResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}
