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
	NumTurns  int             `json:"num_turns,omitempty"`
}

type assistantMessage struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`  // tool_use: tool name
	Input json.RawMessage `json:"input,omitempty"` // tool_use: arguments
}

// buildMCPConfig creates an mcp-config JSON string for streamable HTTP servers.
func buildMCPConfig(servers map[string]MCPServer) string {
	type mcpServer struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers,omitempty"`
	}
	cfg := struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	}{
		MCPServers: make(map[string]mcpServer, len(servers)),
	}
	for name, s := range servers {
		cfg.MCPServers[name] = mcpServer{Type: "http", URL: s.URL, Headers: s.Headers}
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

// expandToolPath expands ~ and env vars inside tool path patterns like "Read(~/src/**)".
func expandToolPath(tool string) string {
	// Tools like "Read(~/src/**)" or just "Bash"
	if i := strings.Index(tool, "("); i != -1 {
		prefix := tool[:i+1]
		rest := tool[i+1:]
		if j := strings.Index(rest, ")"); j != -1 {
			path := rest[:j]
			suffix := rest[j:]
			return prefix + expandPath(path) + suffix
		}
	}
	return tool
}

// expandPath expands ~ and environment variables in a path.
func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + p[1:]
		}
	}
	return os.ExpandEnv(p)
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

		if cfg.SystemPrompt != nil {
			if sp := cfg.SystemPrompt(); sp != "" {
				args = append(args, "--system-prompt", sp)
			}
		}

		if len(cfg.MCPServers) > 0 {
			mcpConfig := buildMCPConfig(cfg.MCPServers)
			args = append(args, "--mcp-config", mcpConfig)
		}

		if len(opts.AllowedTools) > 0 {
			// Expand ~ and env vars in tool path patterns
			var expanded []string
			for _, t := range opts.AllowedTools {
				expanded = append(expanded, expandToolPath(t))
			}
			args = append(args, "--allowedTools", strings.Join(expanded, " "))
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
