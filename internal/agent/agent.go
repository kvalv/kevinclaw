package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"text/template"
	"time"
)

// SessionKey identifies a conversation context. Opaque string — caller decides the format.
type SessionKey string

type MCPServer struct {
	URL     string
	Headers map[string]string // optional, e.g. {"Authorization": "Bearer xxx"}
}

type Config struct {
	IdleTimeout    time.Duration
	WorkDir        string
	SystemPrompt   string
	MCPServers     map[string]MCPServer
	PermissionMode string // e.g. "bypassPermissions"
}

// RunOpts are per-invocation options passed to the Runner.
type RunOpts struct {
	SessionID         string
	DisallowedServers []string // MCP server names to block (e.g. "gcal")
	AllowedTools      []string // if non-empty, restricts built-in tools (overrides Config.AllowedPaths)
}

// Runner executes a claude query and returns stream-json output lines.
type Runner func(ctx context.Context, prompt string, opts RunOpts) ([]string, error)

// SessionStore persists session IDs across restarts.
type SessionStore interface {
	GetSession(ctx context.Context, key string) (string, error)
	SaveSession(ctx context.Context, key, claudeSession string) error
}

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
	policy   ToolPolicy
}

func New(cfg Config) *Agent {
	return &Agent{
		cfg:      cfg,
		runner:   ClaudeRunner(cfg),
		sessions: &memoryStore{sessions: make(map[string]string)},
	}
}

func (a *Agent) Config() Config { return a.cfg }

func (a *Agent) WithRunner(r Runner) *Agent {
	a.runner = r
	return a
}

func (a *Agent) WithSessionStore(s SessionStore) *Agent {
	a.sessions = s
	return a
}

func (a *Agent) WithToolPolicy(p ToolPolicy) *Agent {
	a.policy = p
	return a
}

// Message is a recent message to include as context.
type Message struct {
	UserID    string
	Name      string // display name (e.g. "Kevin"), resolved from Slack
	Text      string
	Timestamp string
}

// MessageOption configures a HandleMessage call.
type MessageOption func(*messageOpts)

type messageOpts struct {
	history []Message
}

// WithHistory prepends recent messages as context.
func WithHistory(msgs []Message) MessageOption {
	return func(o *messageOpts) { o.history = msgs }
}

// HandleMessage sends a prompt to claude and returns the text response.
// userID and channel are used by the policy to determine tool restrictions.
func (a *Agent) HandleMessage(ctx context.Context, key SessionKey, text, userID, channel string, opts ...MessageOption) (string, error) {
	var mo messageOpts
	for _, o := range opts {
		o(&mo)
	}

	prompt := formatPrompt(text, mo.history)

	sessionID, _ := a.sessions.GetSession(ctx, string(key))

	var r Restrictions
	if a.policy != nil {
		r = a.policy(userID, channel)
	}

	slog.Info("agent: handling message",
		"session_key", key,
		"session_id", sessionID,
		"resuming", sessionID != "",
		"prompt_len", len(prompt),
		"history_msgs", len(mo.history),
		"blocked_servers", r.DisallowedServers,
		"restricted_tools", r.AllowedTools != nil,
	)

	start := time.Now()
	lines, err := a.runner(ctx, prompt, RunOpts{
		SessionID:         sessionID,
		DisallowedServers: r.DisallowedServers,
		AllowedTools:      r.AllowedTools,
	})
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

var promptTmpl = template.Must(template.New("prompt").Funcs(template.FuncMap{
	"fmtTime": func(ts string) string {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t.Format("15:04")
		}
		return ts
	},
	"fmtUser": func(m Message) string {
		if m.Name != "" {
			return m.UserID + " (" + m.Name + ")"
		}
		return m.UserID
	},
}).Parse(`{{- if .History -}}
Recent messages:
{{ range .History -}}
[{{ fmtUser . }} {{ fmtTime .Timestamp }}] {{ .Text }}
{{ end }}
{{ end -}}
{{ .Text }}`))

func formatPrompt(text string, history []Message) string {
	if len(history) == 0 {
		return text
	}
	var buf bytes.Buffer
	promptTmpl.Execute(&buf, struct {
		Text    string
		History []Message
	}{Text: text, History: history})
	return buf.String()
}

func parseResponse(lines []string) (result string, sessionID string, err error) {
	for _, line := range lines {
		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			var msg assistantMessage
			if err := json.Unmarshal(ev.Message, &msg); err == nil {
				for _, block := range msg.Content {
					if block.Type == "tool_use" {
						slog.Info("agent: tool call", "tool", block.Name, "input_len", len(block.Input))
					}
				}
			}
		case "result":
			slog.Info("agent: completed", "turns", ev.NumTurns, "status", ev.Subtype)
			if ev.Subtype == "error" {
				return "", "", fmt.Errorf("claude error: %s", ev.Result)
			}
			return ev.Result, ev.SessionID, nil
		}
	}
	return "", "", fmt.Errorf("no result event in response")
}
