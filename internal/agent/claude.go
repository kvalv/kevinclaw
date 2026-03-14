package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// Stream-json types from Claude CLI (--output-format stream-json --verbose).

type streamEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Result    string          `json:"result,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
}

type assistantMessage struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ClaudeRunner returns a Runner that spawns the claude CLI as a subprocess.
func ClaudeRunner(cfg Config) Runner {
	return func(ctx context.Context, prompt string, sessionID string) ([]string, error) {
		args := []string{
			"-p",
			"--output-format", "stream-json",
			"--verbose",
		}

		if sessionID != "" {
			args = append(args, "--resume", sessionID)
		}

		if cfg.SystemPrompt != "" {
			args = append(args, "--system-prompt", cfg.SystemPrompt)
		}

		if cfg.MCPConfigPath != "" {
			args = append(args, "--mcp-config", cfg.MCPConfigPath)
		}

		if len(cfg.AllowedPaths) > 0 {
			var tools []string
			for _, p := range cfg.AllowedPaths {
				tools = append(tools, fmt.Sprintf("Edit(%s/**)", p))
				tools = append(tools, fmt.Sprintf("Write(%s/**)", p))
			}
			tools = append(tools, "Bash", "Read", "Glob", "Grep", "WebSearch", "WebFetch")
			args = append(args, "--allowedTools", strings.Join(tools, " "))

			for _, p := range cfg.AllowedPaths {
				args = append(args, "--add-dir", p)
			}
		}

		if cfg.PermissionMode != "" {
			args = append(args, "--permission-mode", cfg.PermissionMode)
		}

		args = append(args, prompt)

		slog.Debug("claude: spawning", "args_count", len(args), "session_id", sessionID, "workdir", cfg.WorkDir)

		cmd := exec.CommandContext(ctx, "claude", args...)
		if cfg.WorkDir != "" {
			cmd.Dir = cfg.WorkDir
		}

		out, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				slog.Error("claude: exited with error", "code", exitErr.ExitCode(), "stderr", string(exitErr.Stderr))
				return nil, fmt.Errorf("claude exited %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
			}
			return nil, err
		}

		var lines []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line != "" {
				lines = append(lines, line)
			}
		}
		slog.Debug("claude: finished", "output_lines", len(lines))
		return lines, nil
	}
}
