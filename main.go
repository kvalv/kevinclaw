package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kvalv/kevinclaw/internal/agent"
	"github.com/kvalv/kevinclaw/internal/cron"
	"github.com/kvalv/kevinclaw/internal/environment"
	"github.com/kvalv/kevinclaw/internal/postgres"
	"github.com/kvalv/kevinclaw/internal/slack"
	"github.com/kvalv/kevinclaw/migrations"
)

//go:embed KEVIN.md
var kevinPrompt string

func main() {
	setupLogger()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	env, err := environment.New()
	if err != nil {
		return fmt.Errorf("loading environment: %w", err)
	}

	pool, err := setupDB(ctx, env.DATABASE_URL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	d := postgres.New(pool)

	a := agent.New(agent.Config{
		IdleTimeout:    5 * time.Minute,
		WorkDir:        projectRoot(),
		SystemPrompt:   kevinPrompt,
		PermissionMode: "bypassPermissions",
	}).WithSessionStore(d)

	sc := slack.New(env.SLACK_BOT_TOKEN, env.SLACK_APP_TOKEN)

	sched, err := cron.New(ctx, pool, func(ctx context.Context, sessionKey, prompt string) error {
		reply, err := a.HandleMessage(ctx, agent.SessionKey(sessionKey), prompt)
		if err != nil {
			return err
		}
		// TODO: resolve sessionKey back to channel + threadTS for sending
		slog.Info("cron: job completed", "session_key", sessionKey, "reply_len", len(reply))
		_ = reply
		return nil
	})
	if err != nil {
		return fmt.Errorf("cron: %w", err)
	}
	defer sched.Stop(ctx)

	slog.Info("kevinclaw starting")
	return sc.Listen(ctx, func(ev slack.Event) {
		go func() {
			if err := d.SaveMessage(ctx, ev.Channel, ev.ThreadTS, ev.MessageTS, ev.UserID, ev.Text); err != nil {
				slog.Error("db: save message failed", "err", err)
			}

			if err := sc.AddReaction(ctx, ev.Channel, ev.MessageTS, "eyes"); err != nil {
				slog.Warn("eyes reaction failed", "err", err)
			}

			key := agent.SessionKey(ev.Channel + ":" + ev.ThreadTS)
			reply, err := a.HandleMessage(ctx, key, ev.Text)

			if rmErr := sc.RemoveReaction(ctx, ev.Channel, ev.MessageTS, "eyes"); rmErr != nil {
				slog.Warn("remove eyes reaction failed", "err", rmErr)
			}

			if err != nil {
				slog.Error("agent error", "err", err)
				return
			}

			threadTS := ev.ThreadTS
			if threadTS == "" {
				threadTS = ev.MessageTS
			}
			if _, err := sc.SendMessage(ctx, ev.Channel, reply, threadTS); err != nil {
				slog.Error("send error", "err", err)
			}
		}()
	})
}

func setupDB(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging: %w", err)
	}
	slog.Info("db: connected")

	if err := migrations.Run(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	slog.Info("db: migrations applied")
	return pool, nil
}

func projectRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Dir(thisFile)
}
