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
	Channel   string
	MessageTS string // timestamp of this message
	ThreadTS  string // parent thread timestamp (empty if top-level)
	Text      string
	UserID    string
	IsMention bool // true if this was an @mention of the bot
}

// SlackAPI is the subset of the slack.Client we use, for testability.
type SlackAPI interface {
	PostMessageContext(ctx context.Context, channel string, opts ...slack.MsgOption) (string, string, error)
	AddReactionContext(ctx context.Context, name string, item slack.ItemRef) error
	RemoveReactionContext(ctx context.Context, name string, item slack.ItemRef) error
}

// Client wraps the Slack API for sending and receiving messages.
type Client struct {
	api          SlackAPI
	raw          *slack.Client // needed for Socket Mode; nil when using a fake
	appToken     string
	userNames    map[string]string // cache: user ID → display name
	channelNames map[string]string // cache: channel ID → channel name
}

// New creates a new Slack client.
func New(botToken, appToken string) *Client {
	c := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	return &Client{
		api:          c,
		raw:          c,
		appToken:     appToken,
		userNames:    make(map[string]string),
		channelNames: make(map[string]string),
	}
}

// NewWithAPI creates a Client backed by the given API implementation (for testing).
func NewWithAPI(api SlackAPI) *Client {
	return &Client{api: api}
}

// Listen connects via Socket Mode and calls handler for each incoming message.
// Blocks until ctx is cancelled.
func (c *Client) Listen(ctx context.Context, handler func(Event)) error {
	sm := socketmode.New(c.raw)

	seen := make(map[string]bool) // dedup by channel:ts

	dispatch := func(channel, ts, threadTS, text, user, source string) {
		key := channel + ":" + ts
		if seen[key] {
			slog.Debug("slack: dedup skip", "key", key, "source", source)
			return
		}
		seen[key] = true
		// Keep map bounded — clear after 1000 entries
		if len(seen) > 1000 {
			seen = make(map[string]bool)
		}

		slog.Info("slack: event",
			"source", source,
			"channel", channel,
			"user", user,
			"ts", ts,
			"thread_ts", threadTS,
			"text_len", len(text),
		)
		handler(Event{
			Channel:   channel,
			MessageTS: ts,
			ThreadTS:  threadTS,
			Text:      text,
			UserID:    user,
			IsMention: source == "app_mention",
		})
	}

	go func() {
		for evt := range sm.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				ev, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					slog.Warn("slack: unexpected EventsAPI data type")
					continue
				}
				sm.Ack(*evt.Request)

				switch inner := ev.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					if inner.BotID != "" {
						slog.Debug("slack: ignoring bot message", "bot_id", inner.BotID, "channel", inner.Channel)
						continue
					}
					dispatch(inner.Channel, inner.TimeStamp, inner.ThreadTimeStamp, inner.Text, inner.User, "message")
				case *slackevents.AppMentionEvent:
					dispatch(inner.Channel, inner.TimeStamp, inner.ThreadTimeStamp, inner.Text, inner.User, "app_mention")
				default:
					slog.Debug("slack: unhandled inner event", "type", ev.InnerEvent.Type)
				}

			case socketmode.EventTypeConnecting:
				slog.Info("slack: connecting...")
			case socketmode.EventTypeConnected:
				slog.Info("slack: connected")
			case socketmode.EventTypeHello:
				slog.Info("slack: ready")
			default:
				slog.Debug("slack: unhandled socket event", "type", evt.Type)
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
		slog.Error("slack: send failed", "channel", channel, "thread_ts", threadTS, "err", err)
		return "", fmt.Errorf("posting message: %w", err)
	}
	slog.Info("slack: message sent", "channel", channel, "thread_ts", threadTS, "ts", ts, "text_len", len(text))
	return ts, nil
}

// AddReaction adds an emoji reaction to a message.
func (c *Client) AddReaction(ctx context.Context, channel, timestamp, emoji string) error {
	ref := slack.NewRefToMessage(channel, timestamp)
	if err := c.api.AddReactionContext(ctx, emoji, ref); err != nil {
		slog.Error("slack: reaction failed", "channel", channel, "ts", timestamp, "emoji", emoji, "err", err)
		return fmt.Errorf("adding reaction: %w", err)
	}
	slog.Debug("slack: reaction added", "channel", channel, "ts", timestamp, "emoji", emoji)
	return nil
}

// GetUserName returns the display name for a Slack user ID, with caching.
// Returns empty string on error (best-effort).
func (c *Client) GetUserName(userID string) string {
	if name, ok := c.userNames[userID]; ok {
		return name
	}
	if c.raw == nil {
		return ""
	}
	info, err := c.raw.GetUserInfo(userID)
	if err != nil {
		slog.Warn("slack: failed to get user info", "user_id", userID, "err", err)
		return ""
	}
	name := info.Profile.DisplayName
	if name == "" {
		name = info.RealName
	}
	c.userNames[userID] = name
	return name
}

// GetChannelName returns the channel name for a Slack channel ID, with caching.
// Returns empty string on error (best-effort).
func (c *Client) GetChannelName(channelID string) string {
	if name, ok := c.channelNames[channelID]; ok {
		return name
	}
	if c.raw == nil {
		return ""
	}
	info, err := c.raw.GetConversationInfo(&slack.GetConversationInfoInput{ChannelID: channelID})
	if err != nil {
		slog.Warn("slack: failed to get channel info", "channel_id", channelID, "err", err)
		return ""
	}
	c.channelNames[channelID] = info.Name
	return info.Name
}

// RemoveReaction removes an emoji reaction from a message.
func (c *Client) RemoveReaction(ctx context.Context, channel, timestamp, emoji string) error {
	ref := slack.NewRefToMessage(channel, timestamp)
	if err := c.api.RemoveReactionContext(ctx, emoji, ref); err != nil {
		slog.Error("slack: remove reaction failed", "channel", channel, "ts", timestamp, "emoji", emoji, "err", err)
		return fmt.Errorf("removing reaction: %w", err)
	}
	slog.Debug("slack: reaction removed", "channel", channel, "ts", timestamp, "emoji", emoji)
	return nil
}
