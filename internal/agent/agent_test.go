package agent_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kvalv/kevinclaw/internal/agent"
	"github.com/kvalv/kevinclaw/internal/testutil"
)

func TestHandleMessage_SimpleQuestion(t *testing.T) {
	prompt := "what is 2+2? reply with just the number"

	a := agent.New(agent.Config{
		IdleTimeout: 30 * time.Second,
		WorkDir:     t.TempDir(),
	}).WithRunner(testutil.ClaudeVCR(t))

	got, err := a.HandleMessage(t.Context(), agent.SessionKey("test:1"), prompt)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !strings.Contains(got, "4") {
		t.Errorf("expected response containing '4', got: %q", got)
	}
}
