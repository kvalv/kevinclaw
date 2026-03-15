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
	MCPServers     map[string]string // name -> URL (streamable HTTP)
	AllowedPaths   []string
	PermissionMode string // e.g. "bypassPermissions"
}

// Runner executes a claude query and returns stream-json output lines.
// sessionID is empty for new conversations, or a previous session ID to resume.
type Runner func(ctx context.Context, prompt string, sessionID string) ([]string, error)

// SessionStore persists session IDs across restarts.
type SessionStore interface {
	GetSession(ctx context.Context, key string) (string, error)
	SaveSession(ctx context.Context, key, claudeSession string) error
}

// memoryStore is the default in-memory session store.
type memoryStore struct {
	mu       sync.Mutex
	sessions map[string]string
}

func (m *memoryStore) GetSession(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[key], nil
}

func (m *memoryStore) SaveSession(_ context.Context, key, claudeSession string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[key] = claudeSession
	return nil
}

type Agent struct {
	cfg      Config
	runner   Runner
	sessions SessionStore
}

func New(cfg Config) *Agent {
	return &Agent{
		cfg:      cfg,
		runner:   ClaudeRunner(cfg),
		sessions: &memoryStore{sessions: make(map[string]string)},
	}
}

func (a *Agent) WithRunner(r Runner) *Agent {
	a.runner = r
	return a
}

func (a *Agent) WithSessionStore(s SessionStore) *Agent {
	a.sessions = s
	return a
}

// HandleMessage sends a prompt to claude and returns the text response.
// Resumes the session if one exists for this key.
func (a *Agent) HandleMessage(ctx context.Context, key SessionKey, text string) (string, error) {
	sessionID, _ := a.sessions.GetSession(ctx, string(key))

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
		if err := a.sessions.SaveSession(ctx, string(key), newSessionID); err != nil {
			slog.Error("agent: save session failed", "session_key", key, "err", err)
		}
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
