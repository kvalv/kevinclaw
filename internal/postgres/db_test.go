package postgres_test

import (
	"os"
	"testing"

	"github.com/kvalv/kevinclaw/internal/postgres"
	"github.com/kvalv/kevinclaw/internal/testutil"
)

func TestRecentMessages(t *testing.T) {
	if os.Getenv("DB_INTEGRATION") == "" {
		t.Skip("set DB_INTEGRATION=1 to run")
	}

	pool := testutil.NewPostgres(t)
	d := postgres.New(pool)
	ctx := t.Context()

	// Insert some channel messages (no thread)
	d.SaveMessage(ctx, "C123", "", "ts1", "U_ALICE", "Alice", "hello")
	d.SaveMessage(ctx, "C123", "", "ts2", "U_BOB", "Bob", "hey there")
	d.SaveMessage(ctx, "C123", "", "ts3", "U_ALICE", "Alice", "what's up")

	// Insert thread messages
	d.SaveMessage(ctx, "C123", "ts1", "ts1.1", "U_BOB", "Bob", "thread reply 1")
	d.SaveMessage(ctx, "C123", "ts1", "ts1.2", "U_ALICE", "Alice", "thread reply 2")

	// Different channel
	d.SaveMessage(ctx, "C999", "", "ts4", "U_ALICE", "Alice", "other channel")

	t.Run("channel messages", func(t *testing.T) {
		msgs, err := d.RecentMessages(ctx, "C123", "", 10)
		if err != nil {
			t.Fatalf("RecentMessages: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(msgs))
		}
		// Should be oldest first
		if msgs[0].Text != "hello" {
			t.Errorf("first message = %q, want hello", msgs[0].Text)
		}
		if msgs[2].Text != "what's up" {
			t.Errorf("last message = %q, want what's up", msgs[2].Text)
		}
	})

	t.Run("channel messages with limit", func(t *testing.T) {
		msgs, err := d.RecentMessages(ctx, "C123", "", 2)
		if err != nil {
			t.Fatalf("RecentMessages: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		// Should be the 2 most recent, oldest first
		if msgs[0].Text != "hey there" {
			t.Errorf("first = %q, want 'hey there'", msgs[0].Text)
		}
		if msgs[1].Text != "what's up" {
			t.Errorf("second = %q, want 'what's up'", msgs[1].Text)
		}
	})

	t.Run("thread messages", func(t *testing.T) {
		msgs, err := d.RecentMessages(ctx, "C123", "ts1", 10)
		if err != nil {
			t.Fatalf("RecentMessages: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("expected 2 thread messages, got %d", len(msgs))
		}
		if msgs[0].Text != "thread reply 1" {
			t.Errorf("first = %q, want 'thread reply 1'", msgs[0].Text)
		}
	})

	t.Run("empty channel", func(t *testing.T) {
		msgs, err := d.RecentMessages(ctx, "C_EMPTY", "", 10)
		if err != nil {
			t.Fatalf("RecentMessages: %v", err)
		}
		if len(msgs) != 0 {
			t.Fatalf("expected 0 messages, got %d", len(msgs))
		}
	})
}
