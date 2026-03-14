package slack

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Event represents an incoming Slack message.
type Event struct {
	Channel  string
	ThreadTS string
	Text     string
	UserID   string
}

// Client wraps the Slack API for sending and receiving messages.
type Client struct {
	api      *slack.Client
	appToken string
}

// New creates a new Slack client.
func New(botToken, appToken string) *Client {
	return &Client{
		api:      slack.New(botToken, slack.OptionAppLevelToken(appToken)),
		appToken: appToken,
	}
}

// Listen connects via Socket Mode and calls handler for each incoming message.
// Blocks until ctx is cancelled.
func (c *Client) Listen(ctx context.Context, handler func(Event)) error {
	sm := socketmode.New(c.api)

	go func() {
		for evt := range sm.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				ev, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				sm.Ack(*evt.Request)

				switch inner := ev.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					if inner.BotID != "" {
						continue
					}
					handler(Event{
						Channel:  inner.Channel,
						ThreadTS: inner.ThreadTimeStamp,
						Text:     inner.Text,
						UserID:   inner.User,
					})
				case *slackevents.AppMentionEvent:
					handler(Event{
						Channel:  inner.Channel,
						ThreadTS: inner.ThreadTimeStamp,
						Text:     inner.Text,
						UserID:   inner.User,
					})
				}

			case socketmode.EventTypeConnecting:
				slog.Info("slack: connecting...")
			case socketmode.EventTypeConnected:
				slog.Info("slack: connected")
			case socketmode.EventTypeHello:
				slog.Info("slack: hello received")
			default:
				slog.Debug("slack: unhandled event", "type", evt.Type)
				if evt.Request != nil {
					sm.Ack(*evt.Request)
				}
			}
		}
	}()

	return sm.RunContext(ctx)
}

// SendMessage posts a message to a channel, optionally in a thread.
func (c *Client) SendMessage(ctx context.Context, channel, text, threadTS string) (string, error) {
	opts := []slack.MsgOption{
		slack.MsgOptionText(text, false),
	}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	_, ts, err := c.api.PostMessageContext(ctx, channel, opts...)
	if err != nil {
		return "", fmt.Errorf("posting message: %w", err)
	}
	return ts, nil
}
