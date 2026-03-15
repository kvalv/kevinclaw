package agent

import (
	"context"
	"testing"
	"time"
)

func TestClaudeRunner_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires claude CLI")
	}

	var events []StreamEvent
	cfg := Config{
		WorkDir: t.TempDir(),
		OnEvent: func(ev StreamEvent) {
			events = append(events, ev)
			t.Logf("event: key=%s run=%d line=%.100s", ev.SessionKey, ev.RunID, ev.Line)
		},
	}

	runner := ClaudeRunner(cfg)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	lines, err := runner(ctx, "reply with just the word hello", RunOpts{
		SessionKey: "test:pipe",
		RunID:      99,
	})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	t.Logf("lines: %d", len(lines))
	for i, l := range lines {
		t.Logf("  line[%d]: %.100s", i, l)
	}

	if len(lines) == 0 {
		t.Fatal("expected output lines, got none")
	}

	// Should have streaming events matching the lines
	if len(events) == 0 {
		t.Fatal("expected streaming events, got none")
	}
	if len(events) != len(lines) {
		t.Errorf("events=%d lines=%d, expected equal", len(events), len(lines))
	}

	// Check metadata propagation
	for _, ev := range events {
		if ev.SessionKey != "test:pipe" {
			t.Errorf("expected session key 'test:pipe', got %q", ev.SessionKey)
		}
		if ev.RunID != 99 {
			t.Errorf("expected run ID 99, got %d", ev.RunID)
		}
	}

	// Parse should find a result
	result, sessionID, err := parseResponse(lines)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	t.Logf("result: %q session: %s", result, sessionID)

	if result == "" {
		t.Error("expected non-empty result")
	}
}
