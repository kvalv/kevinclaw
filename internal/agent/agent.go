package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// SessionKey identifies a conversation context. Opaque string — caller decides the format.
type SessionKey string

type Config struct {
	IdleTimeout time.Duration
	WorkDir     string
}

// Runner executes a claude query and returns stream-json output lines.
type Runner func(ctx context.Context, prompt string) ([]string, error)

type Agent struct {
	cfg    Config
	runner Runner
}

func New(cfg Config) *Agent {
	return &Agent{cfg: cfg}
}

func (a *Agent) WithRunner(r Runner) *Agent {
	a.runner = r
	return a
}

// HandleMessage sends a prompt to claude and returns the text response.
func (a *Agent) HandleMessage(ctx context.Context, key SessionKey, text string) (string, error) {
	lines, err := a.runner(ctx, text)
	if err != nil {
		return "", fmt.Errorf("running claude: %w", err)
	}
	return parseResponse(lines)
}

type streamEvent struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype,omitempty"`
	Message json.RawMessage `json:"message,omitempty"`
	Result  string          `json:"result,omitempty"`
}

type assistantMessage struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// parseResponse extracts the final text from stream-json lines.
func parseResponse(lines []string) (string, error) {
	for _, line := range lines {
		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type == "result" {
			if ev.Subtype == "error" {
				return "", fmt.Errorf("claude error: %s", ev.Result)
			}
			return ev.Result, nil
		}
	}
	return "", fmt.Errorf("no result event in response")
}
