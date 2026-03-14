package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/kvalv/kevinclaw/internal/agent"
	"github.com/kvalv/kevinclaw/internal/environment"
	"github.com/kvalv/kevinclaw/internal/slack"
)

func main() {
	setupLogger()

	env, err := environment.New()
	if err != nil {
		slog.Error("loading environment", "err", err)
		os.Exit(1)
	}

	a := agent.New(agent.Config{
		IdleTimeout:    5 * time.Minute,
		SystemPrompt:   "You are Kevin, a helpful assistant. Be concise.",
		PermissionMode: "bypassPermissions",
	})

	sc := slack.New(env.SLACK_BOT_TOKEN, env.SLACK_APP_TOKEN)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	slog.Info("kevinclaw starting")
	err = sc.Listen(ctx, func(ev slack.Event) {
		slog.Info("message received", "channel", ev.Channel, "user", ev.UserID, "text", ev.Text)

		key := agent.SessionKey(ev.Channel + ":" + ev.ThreadTS)
		reply, err := a.HandleMessage(ctx, key, ev.Text)
		if err != nil {
			slog.Error("agent error", "err", err)
			return
		}

		// Reply in thread if the message was in a thread, otherwise start a new thread
		threadTS := ev.ThreadTS
		if threadTS == "" {
			threadTS = "" // top-level reply, no thread
		}
		if _, err := sc.SendMessage(ctx, ev.Channel, reply, threadTS); err != nil {
			slog.Error("send error", "err", err)
		}
	})
	if err != nil {
		slog.Error("slack listen", "err", err)
		os.Exit(1)
	}
}
