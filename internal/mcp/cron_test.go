package mcp_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kvalv/kevinclaw/internal/cron"
	mcp "github.com/kvalv/kevinclaw/internal/mcp"
	"github.com/kvalv/kevinclaw/internal/testutil"
)

func TestCronMCP_Schedule(t *testing.T) {
	if os.Getenv("DB_INTEGRATION") == "" {
		t.Skip("set DB_INTEGRATION=1 to run")
	}

	ctx := t.Context()
	pool := testutil.NewPostgres(t)

	done := make(chan struct{})
	var gotKey, gotPrompt string

	sched, err := cron.New(ctx, pool, func(_ context.Context, sessionKey, prompt string) error {
		gotKey = sessionKey
		gotPrompt = prompt
		close(done)
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

	result, err := callTool(ctx, "cron_schedule", map[string]any{
		"session_key": "C123:1234.5678",
		"prompt":      "check deploy status",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for job to execute")
	}

	if gotKey != "C123:1234.5678" {
		t.Errorf("session key = %q, want %q", gotKey, "C123:1234.5678")
	}
	if gotPrompt != "check deploy status" {
		t.Errorf("prompt = %q, want %q", gotPrompt, "check deploy status")
	}
}
