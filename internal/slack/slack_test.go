package slack_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/kvalv/kevinclaw/internal/agent"
	"github.com/kvalv/kevinclaw/internal/environment"
	"github.com/kvalv/kevinclaw/internal/slack"
	"github.com/kvalv/kevinclaw/internal/testutil"
)

func setupClient(t *testing.T) (*slack.Client, environment.Environment) {
	t.Helper()
	if os.Getenv("SLACK_INTEGRATION") == "" {
		t.Skip("set SLACK_INTEGRATION=1 to run")
	}
	env, err := environment.New()
	if err != nil {
		t.Fatalf("loading environment: %v", err)
	}
	return slack.New(env.SLACK_BOT_TOKEN, env.SLACK_APP_TOKEN), env
}

func TestSendDM(t *testing.T) {
	client, env := setupClient(t)

	ts, err := client.SendMessage(t.Context(), env.OWNER_USER_ID, "hello from kevinclaw test", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	t.Logf("sent message ts=%s", ts)
}

type fakeAPI struct {
	reactions []fakeReaction
}

type fakeReaction struct {
	name    string
	channel string
	ts      string
}

func (f *fakeAPI) PostMessageContext(ctx context.Context, channel string, opts ...goslack.MsgOption) (string, string, error) {
	return "", "1234.5678", nil
}

func (f *fakeAPI) AddReactionContext(ctx context.Context, name string, item goslack.ItemRef) error {
	f.reactions = append(f.reactions, fakeReaction{name: name, channel: item.Channel, ts: item.Timestamp})
	return nil
}

func (f *fakeAPI) RemoveReactionContext(ctx context.Context, name string, item goslack.ItemRef) error {
	return nil
}

func TestAddReaction(t *testing.T) {
	fake := &fakeAPI{}
	client := slack.NewWithAPI(fake)

	err := client.AddReaction(t.Context(), "C123", "1234.5678", "eyes")
	if err != nil {
		t.Fatalf("AddReaction: %v", err)
	}

	if len(fake.reactions) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(fake.reactions))
	}
	r := fake.reactions[0]
	if r.name != "eyes" || r.channel != "C123" || r.ts != "1234.5678" {
		t.Errorf("unexpected reaction: %+v", r)
	}
}

func TestSlackRoundTrip(t *testing.T) {
	client, env := setupClient(t)

	a := agent.New(agent.Config{
		IdleTimeout: 30 * time.Second,
		WorkDir:     t.TempDir(),
	}).WithRunner(testutil.ClaudeVCR(t))

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Channel to receive the event
	got := make(chan slack.Event, 1)

	// Start listening in background
	listenErr := make(chan error, 1)
	go func() {
		listenErr <- client.Listen(ctx, func(ev slack.Event) {
			// Only care about messages from the owner
			if ev.UserID == env.OWNER_USER_ID {
				got <- ev
			}
		})
	}()

	// Give Socket Mode a moment to connect
	time.Sleep(2 * time.Second)

	// Send a message as the bot to ourselves — we'll trigger via a thread reply
	// The bot sends a prompt, then we wait for a human message in that thread
	// For testing: send a message that the bot will see as an event
	ts, err := client.SendMessage(ctx, env.OWNER_USER_ID, "kevinclaw test: say something back to me", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	t.Logf("sent prompt ts=%s, waiting for reply event...", ts)

	// Wait for an incoming event
	select {
	case ev := <-got:
		t.Logf("received event: channel=%s text=%q", ev.Channel, ev.Text)

		// Run through agent
		reply, err := a.HandleMessage(ctx, agent.SessionKey(ev.Channel+":"+ev.ThreadTS), ev.Text)
		if err != nil {
			t.Fatalf("HandleMessage: %v", err)
		}

		// Reply in slack
		threadTS := ev.ThreadTS
		if threadTS == "" {
			threadTS = ts
		}
		_, err = client.SendMessage(ctx, ev.Channel, reply, threadTS)
		if err != nil {
			t.Fatalf("reply SendMessage: %v", err)
		}
		if !strings.Contains(reply, "") {
			t.Logf("agent replied: %q", reply)
		}

	case err := <-listenErr:
		t.Fatalf("Listen error: %v", err)

	case <-ctx.Done():
		t.Fatal("timed out waiting for slack event")
	}
}
