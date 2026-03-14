package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SessionKey identifies a conversation context. Opaque string — caller decides the format.
type SessionKey string

type Config struct {
	IdleTimeout    time.Duration
	WorkDir        string
	SystemPrompt   string
	MCPConfigPath  string
	AllowedPaths   []string
	PermissionMode string // e.g. "bypassPermissions"
}

// Runner executes a claude query and returns stream-json output lines.
// sessionID is empty for new conversations, or a previous session ID to resume.
type Runner func(ctx context.Context, prompt string, sessionID string) ([]string, error)

type Agent struct {
	cfg      Config
	runner   Runner
	mu       sync.Mutex
	sessions map[SessionKey]string // key -> claude session ID
}

func New(cfg Config) *Agent {
	return &Agent{
		cfg:      cfg,
		runner:   ClaudeRunner(cfg),
		sessions: make(map[SessionKey]string),
	}
}

func (a *Agent) WithRunner(r Runner) *Agent {
	a.runner = r
	return a
}

// HandleMessage sends a prompt to claude and returns the text response.
// Resumes the session if one exists for this key.
func (a *Agent) HandleMessage(ctx context.Context, key SessionKey, text string) (string, error) {
	a.mu.Lock()
	sessionID := a.sessions[key]
	a.mu.Unlock()

	resuming := sessionID != ""
	slog.Info("agent: handling message",
		"session_key", key,
		"session_id", sessionID,
		"resuming", resuming,
		"prompt_len", len(text),
	)

	start := time.Now()
	lines, err := a.runner(ctx, text, sessionID)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("agent: runner failed", "session_key", key, "elapsed", elapsed, "err", err)
		return "", fmt.Errorf("running claude: %w", err)
	}

	result, newSessionID, err := parseResponse(lines)
	if err != nil {
		slog.Error("agent: parse failed", "session_key", key, "err", err)
		return "", err
	}

	if newSessionID != "" {
		a.mu.Lock()
		a.sessions[key] = newSessionID
		a.mu.Unlock()
	}

	slog.Info("agent: response ready",
		"session_key", key,
		"session_id", newSessionID,
		"elapsed", elapsed,
		"result_len", len(result),
	)

	return result, nil
}

// parseResponse extracts the result text and session ID from stream-json lines.
func parseResponse(lines []string) (result string, sessionID string, err error) {
	for _, line := range lines {
		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type == "result" {
			if ev.Subtype == "error" {
				return "", "", fmt.Errorf("claude error: %s", ev.Result)
			}
			return ev.Result, ev.SessionID, nil
		}
	}
	return "", "", fmt.Errorf("no result event in response")
}
