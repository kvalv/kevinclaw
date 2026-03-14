package cron_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kvalv/kevinclaw/internal/cron"
	"github.com/kvalv/kevinclaw/internal/testutil"
	"github.com/kvalv/kevinclaw/migrations"
)

func TestPromptWorker_Integration(t *testing.T) {
	if os.Getenv("DB_INTEGRATION") == "" {
		t.Skip("set DB_INTEGRATION=1 to run")
	}

	pool := testutil.NewPostgres(t, "")
	if err := migrations.Run(t.Context(), pool); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	done := make(chan struct{})
	var gotKey, gotPrompt string

	sched, err := cron.New(t.Context(), pool, func(_ context.Context, sessionKey, prompt string) error {
		gotKey = sessionKey
		gotPrompt = prompt
		close(done)
		return nil
	})
	if err != nil {
		t.Fatalf("cron.New: %v", err)
	}
	defer sched.Stop(t.Context())

	err = sched.Schedule(t.Context(), cron.PromptJobArgs{
		SessionKey: "C123:1234.5678",
		Prompt:     "check the deploy status",
	})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for job to execute")
	}

	if gotKey != "C123:1234.5678" {
		t.Errorf("session key = %q, want %q", gotKey, "C123:1234.5678")
	}
	if gotPrompt != "check the deploy status" {
		t.Errorf("prompt = %q, want %q", gotPrompt, "check the deploy status")
	}
}
