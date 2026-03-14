package agent_test

import (
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

	// Turn 1: establish a fact
	got, err := a.HandleMessage(t.Context(), key, "The secret number is 42. Just say OK.")
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	t.Logf("turn 1: %q", got)

	// Turn 2: ask it to recall
	got, err = a.HandleMessage(t.Context(), key, "What is the secret number I just told you? Reply with just the number.")
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	t.Logf("turn 2: %q", got)

	if !strings.Contains(got, "42") {
		t.Errorf("expected response containing '42', got: %q", got)
	}
}

func TestHandleMessage_SimpleQuestion(t *testing.T) {
	a := agent.New(agent.Config{
		IdleTimeout: 30 * time.Second,
		WorkDir:     t.TempDir(),
	}).WithRunner(testutil.ClaudeVCR(t))

	got, err := a.HandleMessage(t.Context(), agent.SessionKey("test:1"), "what is 2+2? reply with just the number")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !strings.Contains(got, "4") {
		t.Errorf("expected response containing '4', got: %q", got)
	}
}
