package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
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
	"github.com/kvalv/kevinclaw/web"
)

//go:embed prompts/angela.md
var angelaPrompt string

//go:embed prompts/darryl.md
var darrylPrompt string

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

	// Dashboard (created early so spawner can reference the broker)
	logsDir := filepath.Join(projectRoot(), "memory", "runs")
	os.MkdirAll(logsDir, 0755)
	dashboard := web.NewServer(pool, logsDir)
	go func() {
		slog.Info("dashboard: starting", "addr", "http://localhost:4646/ui")
		if err := http.ListenAndServe(":4646", dashboard.Handler()); err != nil {
			slog.Error("dashboard: failed", "err", err)
		}
	}()

	// Agent spawner — uses indirection so MCP server captures the pointer,
	// and we fill in the real implementation after MCP servers are set up.
	var spawnAgent mcp.AgentSpawner
	spawnerWrapper := func(ctx context.Context, role, prompt string, runID int64, issueID, workDir string) error {
		return spawnAgent(ctx, role, prompt, runID, issueID, workDir)
	}

	mcpServers, mcpShutdown, err := setupMCPServers(ctx, env, cfg, sched, pool, spawnerWrapper)
	if err != nil {
		return fmt.Errorf("mcp: %w", err)
	}
	defer mcpShutdown()

	// Now wire the real spawner with access to MCP server addresses
	angelaMCPs := map[string]agent.MCPServer{
		"bugfix": mcpServers["bugfix"],
		"slack":  mcpServers["slack"],
		"linear": mcpServers["linear"],
	}
	// Darryl's devtools MCP (screenshot upload + dev server management)
	darrylDevtoolsAddr, darrylDevtoolsShutdown, err := mcp.ServeHTTP(ctx, mcp.DevToolsServer("ignite-analytics/main", cfg.Apps), "localhost:0")
	if err != nil {
		return fmt.Errorf("darryl devtools: %w", err)
	}
	defer darrylDevtoolsShutdown()

	darrylMCPs := map[string]agent.MCPServer{
		"bugfix":   mcpServers["bugfix"],
		"slack":    mcpServers["slack"],
		"devtools": {URL: darrylDevtoolsAddr},
		"browser": {
			Command: "npx",
			Args:    []string{"chrome-devtools-mcp", "--executablePath", "/usr/bin/brave", "--headless", "--isolated"},
		},
	}

	appCtx := ctx // capture the app-level context for long-lived agents
	spawnAgent = func(_ context.Context, role, prompt string, runID int64, issueID, workDir string) error {
		// Expand ~ in workDir (Go doesn't do this automatically)
		workDir = agent.ExpandPath(workDir)

		var sp string
		var mcps map[string]agent.MCPServer
		switch role {
		case "angela":
			sp = angelaPrompt
			mcps = angelaMCPs
			if workDir == "" {
				workDir = projectRoot()
			}
		case "darryl":
			sp = darrylPrompt
			mcps = darrylMCPs
			if workDir == "" {
				workDir = projectRoot()
			}
		default:
			return fmt.Errorf("unknown agent role: %s", role)
		}

		sessionKey := fmt.Sprintf("%s:%s", role, issueID)

		// Create persistent log file for this run
		logDir := filepath.Join(projectRoot(), "memory", "runs")
		os.MkdirAll(logDir, 0755)
		logPath := filepath.Join(logDir, fmt.Sprintf("%s-%s.log", issueID, role))
		logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			slog.Error("spawner: failed to create log file", "path", logPath, "err", err)
		}

		runner := agent.ClaudeRunner(agent.Config{
			WorkDir:        workDir,
			SystemPrompt:   func() string { return sp },
			PermissionMode: "bypassPermissions",
			MCPServers:     mcps,
			OnEvent: func(ev agent.StreamEvent) {
				ev.Role = role
				dashboard.Broker().Publish(runID, ev.Role, ev.Line)
				if logFile != nil {
					logFile.WriteString(role + "\t" + ev.Line + "\n")
				}
			},
		})

		go func() {
			if logFile != nil {
				defer logFile.Close()
			}
			slog.Info("spawner: launching agent", "role", role, "issue", issueID, "run_id", runID, "workdir", workDir)
			lines, err := runner(appCtx, prompt, agent.RunOpts{
				SessionKey: sessionKey,
				RunID:      runID,
			})
			if err != nil {
				slog.Error("spawner: agent failed", "role", role, "issue", issueID, "err", err)
				pool.Exec(appCtx, `UPDATE bugfixes SET error=$1 WHERE id=$2`, err.Error(), runID)
				return
			}
			result, _, _ := agent.ParseResponse(lines)
			slog.Info("spawner: agent done", "role", role, "issue", issueID, "result_len", len(result))
		}()

		return nil
	}

	memoryDir := filepath.Join(projectRoot(), "memory")
	systemPrompt := agent.BuildSystemPrompt(memoryDir, time.Now().Format(time.DateOnly))
	a = agent.New(agent.Config{
		IdleTimeout:    5 * time.Minute,
		WorkDir:        projectRoot(),
		SystemPrompt:   func() string { return systemPrompt },
		PermissionMode: "bypassPermissions",
		MCPServers:     mcpServers,
		OnEvent: func(ev agent.StreamEvent) {
			dashboard.Broker().Publish(ev.RunID, "kevin", ev.Line)
		},
	}).
		WithSessionStore(d).
		WithToolPolicy(agent.NewOwnerPolicy(env.OWNER_USER_ID, agent.PolicyPaths{
			Write:  cfg.Paths.Write,
			Read:   cfg.Paths.Read,
			Public: cfg.Paths.Public,
		}))

	agent.StartDailyLogRotation(ctx, memoryDir)
	agent.StartOrchestrator(ctx, pool, a, env.OWNER_USER_ID, 5*time.Minute, 15*time.Minute)

	sc := slack.New(env.SLACK_BOT_TOKEN, env.SLACK_APP_TOKEN, cfg.ActiveChannels)

	logStartupInfo(a)
	return sc.Listen(ctx, func(ev slack.Event) {
		go func() {
			// Always save messages to DB for context
			userName := sc.GetUserName(ev.UserID)
			if err := d.SaveMessage(ctx, ev.Channel, ev.ThreadTS, ev.MessageTS, ev.UserID, userName, ev.Text); err != nil {
				slog.Error("db: save message failed", "err", err)
			}

			if !slack.ShouldHandle(ev, sc.ActiveChannelIDs()) {
				return
			}
			if ev.IsMention {
				if err := sc.AddReaction(ctx, ev.Channel, ev.MessageTS, "eyes"); err != nil {
					slog.Warn("eyes reaction failed", "err", err)
				}
			}

			// Fetch recent messages for context
			history, err := d.RecentMessages(ctx, ev.Channel, ev.ThreadTS, cfg.GetHistoryLimit())
			if err != nil {
				slog.Warn("failed to fetch history", "err", err)
			}

			key := agent.SessionKey(ev.Channel + ":" + ev.ThreadTS)
			reply, err := a.HandleMessage(ctx, key, ev.Text, ev.UserID, ev.Channel, agent.WithHistory(history))

			if ev.IsMention {
				if rmErr := sc.RemoveReaction(ctx, ev.Channel, ev.MessageTS, "eyes"); rmErr != nil {
					slog.Warn("remove eyes reaction failed", "err", rmErr)
				}
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

func setupMCPServers(ctx context.Context, env config.Env, cfg *config.Config, sched *cron.Scheduler, pool *pgxpool.Pool, spawn mcp.AgentSpawner) (map[string]agent.MCPServer, func(), error) {
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

	if err := serve("bugfix", mcp.BugfixServer(pool, spawn)); err != nil {
		return nil, nil, err
	}

	if err := serve("slack", mcp.SlackServer(env.SLACK_BOT_TOKEN)); err != nil {
		return nil, nil, err
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
