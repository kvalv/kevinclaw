package agent

import (
	"bufio"
	"bytes"
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
		URL     string            `json:"url,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
		Command string            `json:"command,omitempty"`
		Args    []string          `json:"args,omitempty"`
	}
	cfg := struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	}{
		MCPServers: make(map[string]mcpServer, len(servers)),
	}
	for name, s := range servers {
		if s.Command != "" {
			cfg.MCPServers[name] = mcpServer{Type: "stdio", Command: s.Command, Args: s.Args}
		} else {
			cfg.MCPServers[name] = mcpServer{Type: "http", URL: s.URL, Headers: s.Headers}
		}
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
			return prefix + ExpandPath(path) + suffix
		}
	}
	return tool
}

// ExpandPath expands ~ and environment variables in a path.
func ExpandPath(p string) string {
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
			args = append(args, "--mcp-config", mcpConfig, "--strict-mcp-config")
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
			args = append(args, "--disallowedTools", strings.Join(patterns, ","))
		}

		args = append(args, prompt)

		cmd := exec.CommandContext(ctx, "claude", args...)
		if cfg.WorkDir != "" {
			cmd.Dir = cfg.WorkDir
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("stdout pipe: %w", err)
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("starting claude: %w", err)
		}
		slog.Info("claude: spawned", "pid", cmd.Process.Pid, "session_id", opts.SessionID, "workdir", cfg.WorkDir)

		// Stream stdout line by line
		var lines []string
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large lines
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			lines = append(lines, line)

			// Emit event for live streaming (skip noisy events)
			if cfg.OnEvent != nil && !strings.Contains(line, `"rate_limit_event"`) {
				cfg.OnEvent(StreamEvent{
					SessionKey: opts.SessionKey,
					RunID:      opts.RunID,
					Line:       line,
				})
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			slog.Error("claude: scanner error", "err", scanErr)
		}

		err = cmd.Wait()
		slog.Info("claude: exited", "pid", cmd.Process.Pid, "exit_code", cmd.ProcessState.ExitCode(), "lines", len(lines), "stderr_len", stderr.Len())

		if stderr.Len() > 0 {
			slog.Debug("claude: stderr", "output", stderr.String())
		}

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				slog.Error("claude: exited with error", "code", exitErr.ExitCode(), "stderr", stderr.String())
				return nil, fmt.Errorf("claude exited %d: %s", exitErr.ExitCode(), stderr.String())
			}
			return nil, err
		}

		return lines, nil
	}
}
