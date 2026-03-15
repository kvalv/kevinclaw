package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/kvalv/kevinclaw/internal/cron"
	mcp "github.com/kvalv/kevinclaw/internal/mcp"
	"github.com/kvalv/kevinclaw/internal/testutil"
)

func TestCronMCP(t *testing.T) {
	if os.Getenv("DB_INTEGRATION") == "" {
		t.Skip("set DB_INTEGRATION=1 to run")
	}

	ctx := t.Context()
	pool := testutil.NewPostgres(t)

	done := make(chan struct{}, 1)
	var gotKey, gotPrompt string

	sched, err := cron.New(ctx, pool, func(_ context.Context, sessionKey, prompt string) error {
		gotKey = sessionKey
		gotPrompt = prompt
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cron.New: %v", err)
	}
	defer sched.Stop(ctx)

	callTool, cleanup, err := mcp.TestClient(ctx, mcp.CronServer(sched))
	if err != nil {
		t.Fatalf("TestClient: %v", err)
	}
	defer cleanup()

	t.Run("schedule", func(t *testing.T) {
		result, err := callTool(ctx, "cron_schedule", map[string]any{
			"session_key": "C123:1234.5678",
			"prompt":      "check deploy status",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if result.IsError {
			t.Fatalf("tool error: %v", result.Content)
		}

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for job")
		}

		if gotKey != "C123:1234.5678" {
			t.Errorf("session key = %q, want %q", gotKey, "C123:1234.5678")
		}
		if gotPrompt != "check deploy status" {
			t.Errorf("prompt = %q, want %q", gotPrompt, "check deploy status")
		}
	})

	t.Run("list", func(t *testing.T) {
		// Schedule another job so there's something to list
		result, err := callTool(ctx, "cron_schedule", map[string]any{
			"session_key": "C456:5678.9012",
			"prompt":      "list test job",
		})
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		if result.IsError {
			t.Fatalf("schedule error: %v", result.Content)
		}

		// Wait for it to be picked up
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}

		result, err = callTool(ctx, "cron_list", map[string]any{})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if result.IsError {
			t.Fatalf("tool error: %v", result.Content)
		}
		// Should return valid JSON array
		text := result.Content[0].(*mcp.TextContent).Text
		var jobs []json.RawMessage
		if err := json.Unmarshal([]byte(text), &jobs); err != nil {
			t.Fatalf("invalid JSON: %v (got %q)", err, text)
		}
		t.Logf("listed %d jobs", len(jobs))
	})

	t.Run("cancel", func(t *testing.T) {
		// Schedule a job
		result, err := callTool(ctx, "cron_schedule", map[string]any{
			"session_key": "C789:cancel.test",
			"prompt":      "should be cancelled",
		})
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}

		// List to find the job ID
		result, err = callTool(ctx, "cron_list", map[string]any{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var jobs []struct {
			ID    int64  `json:"id"`
			State string `json:"state"`
		}
		text := result.Content[0].(*mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &jobs); err != nil {
			t.Fatalf("parse jobs: %v", err)
		}

		// Find a non-completed job to cancel
		var jobID int64
		for _, j := range jobs {
			if j.State != "completed" {
				jobID = j.ID
				break
			}
		}
		if jobID == 0 {
			t.Skip("no cancellable job found")
		}

		result, err = callTool(ctx, "cron_cancel", map[string]any{"job_id": float64(jobID)})
		if err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if result.IsError {
			t.Fatalf("cancel error: %v", result.Content)
		}
		t.Logf("cancelled job %d", jobID)
	})
}
