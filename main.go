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
	"github.com/kvalv/kevinclaw/internal/config"
	"github.com/kvalv/kevinclaw/internal/cron"
	"github.com/kvalv/kevinclaw/internal/gcal"
	"github.com/kvalv/kevinclaw/internal/mcp"
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
	env, err := config.LoadEnv()
	if err != nil {
		return fmt.Errorf("loading environment: %w", err)
	}

	cfg, err := config.Load(filepath.Join(projectRoot(), "kevin.yaml"))
	if err != nil {
		return fmt.Errorf("loading kevin.yaml: %w", err)
	}

	pool, err := setupDB(ctx, env.DATABASE_URL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	d := postgres.New(pool)

	// Agent is referenced by the cron handler, so we use a pointer that's set after creation.
	var a *agent.Agent

	sched, err := cron.New(ctx, pool, func(ctx context.Context, sessionKey, prompt string) error {
		reply, err := a.HandleMessage(ctx, agent.SessionKey(sessionKey), prompt, env.OWNER_USER_ID, "")
		if err != nil {
			return err
		}
		slog.Info("cron: job completed", "session_key", sessionKey, "reply_len", len(reply))
		return nil
	})
	if err != nil {
		return fmt.Errorf("cron: %w", err)
	}
	defer sched.Stop(ctx)

	mcpServers, mcpShutdown, err := setupMCPServers(ctx, env, cfg, sched)
	if err != nil {
		return fmt.Errorf("mcp: %w", err)
	}
	defer mcpShutdown()

	a = agent.New(agent.Config{
		IdleTimeout:    5 * time.Minute,
		WorkDir:        projectRoot(),
		SystemPrompt:   kevinPrompt,
		PermissionMode: "bypassPermissions",
		MCPServers:     mcpServers,
	}).
		WithSessionStore(d).
		WithToolPolicy(agent.NewOwnerPolicy(env.OWNER_USER_ID, agent.PolicyPaths{
			Write:  cfg.Paths.Write,
			Read:   cfg.Paths.Read,
			Public: cfg.Paths.Public,
		}))

	sc := slack.New(env.SLACK_BOT_TOKEN, env.SLACK_APP_TOKEN)

	logStartupInfo(a)
	return sc.Listen(ctx, func(ev slack.Event) {
		go func() {
			userName := sc.GetUserName(ev.UserID)
			if err := d.SaveMessage(ctx, ev.Channel, ev.ThreadTS, ev.MessageTS, ev.UserID, userName, ev.Text); err != nil {
				slog.Error("db: save message failed", "err", err)
			}

			if err := sc.AddReaction(ctx, ev.Channel, ev.MessageTS, "eyes"); err != nil {
				slog.Warn("eyes reaction failed", "err", err)
			}

			// Fetch recent messages for context
			history, err := d.RecentMessages(ctx, ev.Channel, ev.ThreadTS, cfg.GetHistoryLimit())
			if err != nil {
				slog.Warn("failed to fetch history", "err", err)
			}

			key := agent.SessionKey(ev.Channel + ":" + ev.ThreadTS)
			reply, err := a.HandleMessage(ctx, key, ev.Text, ev.UserID, ev.Channel, agent.WithHistory(history))

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

func setupMCPServers(ctx context.Context, env config.Env, cfg *config.Config, sched *cron.Scheduler) (map[string]agent.MCPServer, func(), error) {
	servers := make(map[string]agent.MCPServer)
	var shutdowns []func()

	serve := func(name string, s *mcp.Server) error {
		addr, shutdown, err := mcp.ServeHTTP(ctx, s, "localhost:0")
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		shutdowns = append(shutdowns, shutdown)
		servers[name] = agent.MCPServer{URL: addr}
		return nil
	}

	if err := serve("debug", mcp.DebugServer()); err != nil {
		return nil, nil, err
	}

	if err := serve("cron", mcp.CronServer(sched)); err != nil {
		return nil, nil, err
	}

	if env.GOOGLE_REFRESH_TOKEN != "" {
		if err := serve("gcal", mcp.GCalServer(gcal.New(env.GOOGLE_CLIENT_ID, env.GOOGLE_CLIENT_SECRET, env.GOOGLE_REFRESH_TOKEN))); err != nil {
			return nil, nil, err
		}
	}

	if len(cfg.HomeAssistant.Entities) > 0 && env.HOMEASSISTANT_API_URL != "" {
		if err := serve("homeassistant", mcp.HomeAssistantServer(cfg.HomeAssistant.Entities, env.HOMEASSISTANT_API_URL, env.HOMEASSISTANT_API_TOKEN)); err != nil {
			return nil, nil, err
		}
	}

	if env.LINEAR_API_KEY != "" {
		servers["linear"] = agent.MCPServer{
			URL:     "https://mcp.linear.app/mcp",
			Headers: map[string]string{"Authorization": "Bearer " + env.LINEAR_API_KEY},
		}
	}

	shutdown := func() {
		for _, fn := range shutdowns {
			fn()
		}
	}
	return servers, shutdown, nil
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

func logStartupInfo(a *agent.Agent) {
	cfg := a.Config()

	var skills []string
	skillsDir := filepath.Join(cfg.WorkDir, ".claude", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				skills = append(skills, e.Name())
			}
		}
	}

	var mcpNames []string
	for name := range cfg.MCPServers {
		mcpNames = append(mcpNames, name)
	}

	slog.Info("kevinclaw starting",
		"skills", skills,
		"mcp_servers", mcpNames,
		"workdir", cfg.WorkDir,
	)
}

func projectRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Dir(thisFile)
}
