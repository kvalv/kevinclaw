package slack_test

import (
	"os"
	"testing"

	"github.com/kvalv/kevinclaw/internal/environment"
	"github.com/kvalv/kevinclaw/internal/slack"
)

func TestSendDM(t *testing.T) {
	if os.Getenv("SLACK_INTEGRATION") == "" {
		t.Skip("set SLACK_INTEGRATION=1 to run")
	}
	env, err := environment.New()
	if err != nil {
		t.Fatalf("loading environment: %v", err)
	}

	client := slack.New(env.SLACK_BOT_TOKEN)
	ts, err := client.SendMessage(t.Context(), env.OWNER_USER_ID, "hello from kevinclaw test", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	t.Logf("sent message ts=%s", ts)
}
