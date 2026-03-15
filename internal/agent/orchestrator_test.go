package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kvalv/kevinclaw/internal/agent"
	"github.com/kvalv/kevinclaw/internal/testutil"
)

func TestResumeRunningBugfixes(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	pool := testutil.NewPostgres(t)
	ctx := t.Context()

	// Insert a bugfix in "running" state (simulating a previous run that was interrupted)
	_, err := pool.Exec(ctx,
		`INSERT INTO bugfixes (linear_issue_id, title, status, worktree_path, branch, started_at)
		 VALUES ('PLA-99', 'Test resume bug', 'running', '/tmp/wt', 'kevin/PLA-99-test', now())`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	okResult := `{"type":"result","subtype":"success","result":"resumed","session_id":"s1"}`

	var gotPrompt string
	var gotRunID int64
	capture := func(_ context.Context, prompt string, opts agent.RunOpts) ([]string, error) {
		gotPrompt = prompt
		gotRunID = opts.RunID
		return []string{okResult}, nil
	}

	a := agent.New(agent.Config{}).WithRunner(capture)

	agent.StartOrchestrator(ctx, pool, a, "U_OWNER", 1*time.Hour, 1*time.Hour)

	// Give the goroutine a moment to run
	time.Sleep(500 * time.Millisecond)

	if gotPrompt == "" {
		t.Fatal("expected resume prompt, got nothing")
	}
	if !strings.Contains(gotPrompt, "PLA-99") {
		t.Errorf("expected PLA-99 in prompt, got: %q", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "restarted") {
		t.Errorf("expected 'restarted' in prompt, got: %q", gotPrompt)
	}
	if gotRunID == 0 {
		t.Error("expected non-zero RunID")
	}
}
