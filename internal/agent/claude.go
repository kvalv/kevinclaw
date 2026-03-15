package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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

// buildMCPConfig creates an mcp-config JSON string for streamable HTTP servers.
func buildMCPConfig(servers map[string]string) string {
	type mcpServer struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	cfg := struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	}{
		MCPServers: make(map[string]mcpServer, len(servers)),
	}
	for name, url := range servers {
		cfg.MCPServers[name] = mcpServer{Type: "http", URL: url}
	}
	// Write to temp file — claude CLI expects a file path or JSON string
	f, err := os.CreateTemp("", "kevinclaw-mcp-*.json")
	if err != nil {
		slog.Error("claude: failed to create mcp config", "err", err)
		return ""
	}
	json.NewEncoder(f).Encode(cfg)
	f.Close()
	return f.Name()
}

// ClaudeRunner returns a Runner that spawns the claude CLI as a subprocess.
func ClaudeRunner(cfg Config) Runner {
	return func(ctx context.Context, prompt string, opts RunOpts) ([]string, error) {
		args := []string{
			"-p",
			"--output-format", "stream-json",
			"--verbose",
		}

		if opts.SessionID != "" {
			args = append(args, "--resume", opts.SessionID)
		}

		if cfg.SystemPrompt != "" {
			args = append(args, "--system-prompt", cfg.SystemPrompt)
		}

		if len(cfg.MCPServers) > 0 {
			mcpConfig := buildMCPConfig(cfg.MCPServers)
			args = append(args, "--mcp-config", mcpConfig)
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

		if len(opts.DisallowedServers) > 0 {
			var patterns []string
			for _, name := range opts.DisallowedServers {
				patterns = append(patterns, fmt.Sprintf("mcp__%s__*", name))
			}
			args = append(args, "--disallowedTools", strings.Join(patterns, " "))
		}

		args = append(args, prompt)

		slog.Debug("claude: spawning", "args_count", len(args), "session_id", opts.SessionID, "workdir", cfg.WorkDir)

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
