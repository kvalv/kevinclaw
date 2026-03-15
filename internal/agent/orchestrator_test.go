package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kvalv/kevinclaw/internal/agent"
	"github.com/kvalv/kevinclaw/internal/testutil"
)

func TestResumeUnfinishedBugfixes(t *testing.T) {
	if testing.Short() {
		t.Skip("requires postgres")
	}
	pool := testutil.NewPostgres(t)
	ctx := t.Context()

	// Insert bugfixes in various unfinished states
	_, err := pool.Exec(ctx,
		`INSERT INTO bugfixes (linear_issue_id, title, status, worktree_path, branch, started_at)
		 VALUES ('PLA-99', 'Test resume bug', 'running', '/tmp/wt', 'kevin/PLA-99-test', now())`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	okResult := `{"type":"result","subtype":"success","result":"ok","session_id":"s1"}`

	var gotPrompt string
	capture := func(_ context.Context, prompt string, opts agent.RunOpts) ([]string, error) {
		gotPrompt = prompt
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
	if !strings.Contains(gotPrompt, "running") {
		t.Errorf("expected status in prompt, got: %q", gotPrompt)
	}
}
