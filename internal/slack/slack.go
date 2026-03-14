package slack

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

// Client wraps the Slack API for sending and receiving messages.
type Client struct {
	api *slack.Client
}

// New creates a new Slack client from a bot token.
func New(botToken string) *Client {
	return &Client{
		api: slack.New(botToken),
	}
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

// SendDM sends a direct message to a user by opening/reusing a DM channel.
func (c *Client) SendDM(ctx context.Context, userID, text string) (string, error) {
	ch, _, _, err := c.api.OpenConversationContext(ctx, &slack.OpenConversationParameters{
		Users: []string{userID},
	})
	if err != nil {
		return "", fmt.Errorf("opening DM: %w", err)
	}
	return c.SendMessage(ctx, ch.ID, text, "")
}
