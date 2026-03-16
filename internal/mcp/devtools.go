package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DevToolsServer creates an MCP server with dev server management and screenshot upload tools.
func DevToolsServer(repo string) *sdkmcp.Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "kevinclaw-devtools", Version: "v0.0.1"}, nil)

	var mu sync.Mutex
	var devServerCmd *exec.Cmd

	// upload_screenshot — uploads a file to the GitHub screenshots release, returns URL
	s.AddTool(&sdkmcp.Tool{
		Name:        "upload_screenshot",
		Description: "Upload a screenshot to GitHub (org-private) and return the URL for use in PR comments.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file_path": {"type": "string", "description": "Absolute path to the PNG file"},
				"name":      {"type": "string", "description": "Name for the upload (e.g. PLA-11-before). .png added if missing."}
			},
			"required": ["file_path"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			FilePath string `json:"file_path"`
			Name     string `json:"name"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}

		if _, err := os.Stat(args.FilePath); err != nil {
			return errResult("file not found: %v", err), nil
		}

		name := args.Name
		if name == "" {
			name = filepath.Base(args.FilePath)
		}
		if !strings.HasSuffix(name, ".png") {
			name += ".png"
		}

		// Upload to screenshots release
		tag := "screenshots"
		uploadCmd := exec.CommandContext(ctx, "gh", "release", "upload", tag, args.FilePath,
			"--repo", repo, "--clobber")
		if out, err := uploadCmd.CombinedOutput(); err != nil {
			return errResult("upload failed: %s %v", string(out), err), nil
		}

		// Get the download URL
		urlCmd := exec.CommandContext(ctx, "gh", "release", "view", tag,
			"--repo", repo, "--json", "assets",
			"--jq", fmt.Sprintf(`.assets[] | select(.name == "%s") | .url`, name))
		out, err := urlCmd.Output()
		if err != nil {
			return errResult("failed to get URL: %v", err), nil
		}

		url := strings.TrimSpace(string(out))
		if url == "" {
			return errResult("upload succeeded but URL not found for %s", name), nil
		}

		return textResult("%s", url), nil
	})

	// dev_server_start — starts npm run dev:proxy in an app directory
	s.AddTool(&sdkmcp.Tool{
		Name:        "dev_server_start",
		Description: "Start the frontend dev server for an app. Runs npm install + npm run dev:proxy in the app directory.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"worktree_path": {"type": "string", "description": "Path to the git worktree root"},
				"app":           {"type": "string", "description": "App name under apps/ (e.g. company-settings, contracts, suppliers)"}
			},
			"required": ["worktree_path", "app"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			WorktreePath string `json:"worktree_path"`
			App          string `json:"app"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}

		appDir := filepath.Join(args.WorktreePath, "apps", args.App)
		if _, err := os.Stat(appDir); err != nil {
			return errResult("app directory not found: %s", appDir), nil
		}

		mu.Lock()
		defer mu.Unlock()

		// Kill existing server if running
		if devServerCmd != nil && devServerCmd.Process != nil {
			devServerCmd.Process.Kill()
			devServerCmd = nil
		}

		// npm install
		installCmd := exec.CommandContext(ctx, "npm", "install")
		installCmd.Dir = appDir
		if out, err := installCmd.CombinedOutput(); err != nil {
			return errResult("npm install failed: %s", string(out)), nil
		}

		// Start dev:proxy in background
		cmd := exec.Command("npm", "run", "dev:proxy")
		cmd.Dir = appDir
		cmd.Env = append(os.Environ(),
			"REACT_APP_DEFAULT_USER="+os.Getenv("REACT_APP_DEFAULT_USER"),
			"REACT_APP_DEFAULT_PASSWORD="+os.Getenv("REACT_APP_DEFAULT_PASSWORD"),
		)
		if err := cmd.Start(); err != nil {
			return errResult("failed to start dev server: %v", err), nil
		}
		devServerCmd = cmd
		slog.Info("devtools: dev server started", "app", args.App, "pid", cmd.Process.Pid, "dir", appDir)

		// Wait for server to be ready
		ready := false
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			checkCmd := exec.CommandContext(ctx, "curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://localhost:3000")
			if out, err := checkCmd.Output(); err == nil && strings.TrimSpace(string(out)) == "200" {
				ready = true
				break
			}
		}

		if !ready {
			return textResult("Dev server started (PID %d) but localhost:3000 not responding yet. It may still be starting up.", cmd.Process.Pid), nil
		}

		return textResult("Dev server ready at http://localhost:3000 (PID %d)", cmd.Process.Pid), nil
	})

	// dev_server_stop — stops the running dev server
	s.AddTool(&sdkmcp.Tool{
		Name:        "dev_server_stop",
		Description: "Stop the running frontend dev server.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		mu.Lock()
		defer mu.Unlock()

		if devServerCmd == nil || devServerCmd.Process == nil {
			return textResult("No dev server running."), nil
		}

		pid := devServerCmd.Process.Pid
		devServerCmd.Process.Kill()
		devServerCmd = nil
		slog.Info("devtools: dev server stopped", "pid", pid)

		return textResult("Dev server stopped (PID %d).", pid), nil
	})

	return s
}
