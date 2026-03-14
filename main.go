package main

import (
	"context"
	_ "embed"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kvalv/kevinclaw/internal/agent"
	"github.com/kvalv/kevinclaw/internal/environment"
	"github.com/kvalv/kevinclaw/internal/slack"
)

//go:embed KEVIN.md
var kevinPrompt string

func projectRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Dir(thisFile)
}

func main() {
	setupLogger()

	env, err := environment.New()
	if err != nil {
		slog.Error("loading environment", "err", err)
		os.Exit(1)
	}

	a := agent.New(agent.Config{
		IdleTimeout:    5 * time.Minute,
		WorkDir:        projectRoot(),
		SystemPrompt:   kevinPrompt,
		PermissionMode: "bypassPermissions",
	})

	sc := slack.New(env.SLACK_BOT_TOKEN, env.SLACK_APP_TOKEN)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	slog.Info("kevinclaw starting")
	err = sc.Listen(ctx, func(ev slack.Event) {
		go func() {
			// Add :eyes: to acknowledge receipt
			if err := sc.AddReaction(ctx, ev.Channel, ev.MessageTS, "eyes"); err != nil {
				slog.Warn("eyes reaction failed", "err", err)
			}

			key := agent.SessionKey(ev.Channel + ":" + ev.ThreadTS)
			reply, err := a.HandleMessage(ctx, key, ev.Text)

			// Remove :eyes: once done (whether success or failure)
			if rmErr := sc.RemoveReaction(ctx, ev.Channel, ev.MessageTS, "eyes"); rmErr != nil {
				slog.Warn("remove eyes reaction failed", "err", rmErr)
			}

			if err != nil {
				slog.Error("agent error", "err", err)
				return
			}

			// Reply in existing thread, or start a new thread under the original message
			threadTS := ev.ThreadTS
			if threadTS == "" {
				threadTS = ev.MessageTS
			}
			if _, err := sc.SendMessage(ctx, ev.Channel, reply, threadTS); err != nil {
				slog.Error("send error", "err", err)
			}
		}()
	})
	if err != nil {
		slog.Error("slack listen", "err", err)
		os.Exit(1)
	}
}
