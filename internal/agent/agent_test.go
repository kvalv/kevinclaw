package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kvalv/kevinclaw/internal/agent"
	"github.com/kvalv/kevinclaw/internal/testutil"
)

func TestHandleMessage_MultiTurn(t *testing.T) {
	a := agent.New(agent.Config{
		IdleTimeout: 30 * time.Second,
		WorkDir:     t.TempDir(),
	}).WithRunner(testutil.ClaudeVCR(t))

	key := agent.SessionKey("test:multi")

	got, err := a.HandleMessage(t.Context(), key, "The secret number is 42. Just say OK.", "U123", "C123")
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	t.Logf("turn 1: %q", got)

	got, err = a.HandleMessage(t.Context(), key, "What is the secret number I just told you? Reply with just the number.", "U123", "C123")
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	t.Logf("turn 2: %q", got)

	if !strings.Contains(got, "42") {
		t.Errorf("expected response containing '42', got: %q", got)
	}
}

func TestHandleMessage_PolicyBlocksServers(t *testing.T) {
	okResult := `{"type":"result","subtype":"success","result":"ok","session_id":"s1"}`

	var gotOpts agent.RunOpts
	captureOpts := func(_ context.Context, _ string, opts agent.RunOpts) ([]string, error) {
		gotOpts = opts
		return []string{okResult}, nil
	}

	a := agent.New(agent.Config{}).WithRunner(captureOpts).WithToolPolicy(agent.NewOwnerPolicy("owner", agent.PolicyPaths{
		Read:   []string{"~/src"},
		Write:  []string{"~/src"},
		Public: []string{"~/src/docs"},
	}))

	t.Run("non-owner gets restricted", func(t *testing.T) {
		a.HandleMessage(t.Context(), "k1", "hi", "stranger", "C123")
		if len(gotOpts.DisallowedServers) == 0 {
			t.Fatal("expected blocked servers for non-owner")
		}
		if gotOpts.AllowedTools == nil {
			t.Fatal("expected restricted tools for non-owner")
		}
	})

	t.Run("owner gets scoped but not blocked", func(t *testing.T) {
		a.HandleMessage(t.Context(), "k2", "hi", "owner", "C123")
		if len(gotOpts.DisallowedServers) != 0 {
			t.Fatalf("expected 0 blocked servers, got %v", gotOpts.DisallowedServers)
		}
		// Owner has scoped tools (not nil) but includes Bash, Skill, etc.
		if gotOpts.AllowedTools == nil {
			t.Fatal("expected scoped tools for owner")
		}
	})
}

func TestHandleMessage_WithContext(t *testing.T) {
	okResult := `{"type":"result","subtype":"success","result":"ok","session_id":"s1"}`

	var gotPrompt string
	capture := func(_ context.Context, prompt string, _ agent.RunOpts) ([]string, error) {
		gotPrompt = prompt
		return []string{okResult}, nil
	}

	a := agent.New(agent.Config{}).WithRunner(capture)

	history := []agent.Message{
		{UserID: "U_ALICE", Text: "the deploy failed", Timestamp: "2026-03-15T09:00:00Z"},
		{UserID: "U_BOB", Text: "which service?", Timestamp: "2026-03-15T09:01:00Z"},
	}

	a.HandleMessage(t.Context(), "k1", "fix it", "U_ALICE", "C123", agent.WithHistory(history))

	if !strings.Contains(gotPrompt, "the deploy failed") {
		t.Errorf("expected context in prompt, got: %q", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "which service?") {
		t.Errorf("expected context in prompt, got: %q", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "fix it") {
		t.Errorf("expected actual message in prompt, got: %q", gotPrompt)
	}
	// Context should come before the actual message
	deployIdx := strings.Index(gotPrompt, "the deploy failed")
	fixIdx := strings.Index(gotPrompt, "fix it")
	if deployIdx > fixIdx {
		t.Errorf("context should come before message, deploy at %d, fix at %d", deployIdx, fixIdx)
	}
}

func TestHandleMessage_SimpleQuestion(t *testing.T) {
	a := agent.New(agent.Config{
		IdleTimeout: 30 * time.Second,
		WorkDir:     t.TempDir(),
	}).WithRunner(testutil.ClaudeVCR(t))

	got, err := a.HandleMessage(t.Context(), agent.SessionKey("test:1"), "what is 2+2? reply with just the number", "U123", "C123")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !strings.Contains(got, "4") {
		t.Errorf("expected response containing '4', got: %q", got)
	}
}
