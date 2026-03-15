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

	a := agent.New(agent.Config{}).WithRunner(captureOpts).WithPolicy(agent.NewOwnerPolicy("owner"))

	t.Run("non-owner gets restricted", func(t *testing.T) {
		a.HandleMessage(t.Context(), "k1", "hi", "stranger", "C123")
		if len(gotOpts.DisallowedServers) == 0 {
			t.Fatal("expected blocked servers for non-owner")
		}
		if gotOpts.AllowedTools == nil {
			t.Fatal("expected restricted tools for non-owner")
		}
	})

	t.Run("owner gets everything", func(t *testing.T) {
		a.HandleMessage(t.Context(), "k2", "hi", "owner", "C123")
		if len(gotOpts.DisallowedServers) != 0 {
			t.Fatalf("expected 0 blocked servers, got %v", gotOpts.DisallowedServers)
		}
		if gotOpts.AllowedTools != nil {
			t.Fatalf("expected nil allowed tools, got %v", gotOpts.AllowedTools)
		}
	})
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
