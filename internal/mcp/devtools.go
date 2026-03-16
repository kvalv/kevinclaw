package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/kvalv/kevinclaw/internal/config"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DevToolsServer creates an MCP server with dev server management and screenshot upload tools.
func DevToolsServer(repo string, apps map[string]config.AppDevConfig) *sdkmcp.Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "kevinclaw-devtools", Version: "v0.0.1"}, nil)

	ds := newDevServer(apps, shellRunner)

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

	// dev_server_start — runs setup + dev server for an app
	s.AddTool(&sdkmcp.Tool{
		Name:        "dev_server_start",
		Description: "Start the frontend dev server for an app. Runs setup commands then the dev command from config.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"worktree_path": {"type": "string", "description": "Path to the git worktree root"},
				"app":           {"type": "string", "description": "App name (e.g. company-settings, contracts, suppliers)"}
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

		if err := ds.Start(ctx, args.WorktreePath, args.App); err != nil {
			return errResult("dev server start failed: %v", err), nil
		}

		cfg := ds.apps[args.App]
		path := cfg.Path
		if path == "" {
			path = "/"
		}
		return textResult("Dev server started for %s at http://localhost:%d%s", args.App, cfg.Port, path), nil
	})

	// dev_server_stop — stops the running dev server
	s.AddTool(&sdkmcp.Tool{
		Name:        "dev_server_stop",
		Description: "Stop the running frontend dev server.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if err := ds.Stop(); err != nil {
			return errResult("stop failed: %v", err), nil
		}
		return textResult("Dev server stopped."), nil
	})

	return s
}

// shellRunner executes a shell command in the given directory.
func shellRunner(dir string, cmd string) error {
	parts := strings.Fields(cmd)
	c := exec.Command(parts[0], parts[1:]...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", cmd, string(out))
	}
	return nil
}

// ErrPortBusy is returned when the app's port is already in use.
var ErrPortBusy = errors.New("port is busy")

// cmdRunner runs a shell command in the given directory. Blocks until done.
type cmdRunner func(dir string, cmd string) error

// devServer manages frontend dev server lifecycle.
type devServer struct {
	apps   map[string]config.AppDevConfig
	runner cmdRunner
	mu     sync.Mutex
	cmd    *exec.Cmd
}

// newDevServer creates a devServer with the given app configs and command runner.
func newDevServer(apps map[string]config.AppDevConfig, runner cmdRunner) *devServer {
	return &devServer{apps: apps, runner: runner}
}

// Start runs setup commands then launches the dev command in the background.
func (ds *devServer) Start(ctx context.Context, worktreePath string, app string) error {
	cfg, ok := ds.apps[app]
	if !ok {
		return fmt.Errorf("unknown app %q", app)
	}

	// Check if the port is available (skip for port 0, used in tests)
	if cfg.Port != 0 {
		ln, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", cfg.Port))
		if err != nil {
			slog.Error("devtools: port busy", "app", app, "port", cfg.Port, "err", err)
			return fmt.Errorf("port %d: %w", cfg.Port, ErrPortBusy)
		}
		ln.Close()
	}

	appDir := filepath.Join(worktreePath, "apps", app)

	// Run setup commands synchronously
	for _, cmd := range cfg.SetupCmds {
		if err := ds.runner(appDir, cmd); err != nil {
			return fmt.Errorf("setup command %q failed: %w", cmd, err)
		}
	}

	// Launch dev command in background
	ds.mu.Lock()
	defer ds.mu.Unlock()

	parts := strings.Fields(cfg.DevCmd)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = appDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("dev command %q failed: %w", cfg.DevCmd, err)
	}
	ds.cmd = cmd
	slog.Info("devtools: dev server started", "app", app, "pid", cmd.Process.Pid, "dir", appDir)

	// Kill on context cancel
	go func() {
		<-ctx.Done()
		ds.Stop()
	}()

	return nil
}

// Stop kills the running dev server process.
func (ds *devServer) Stop() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.cmd == nil || ds.cmd.Process == nil {
		return nil
	}

	pid := ds.cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err == nil {
		syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		ds.cmd.Process.Kill()
	}
	ds.cmd.Wait()
	ds.cmd = nil

	slog.Info("devtools: dev server stopped", "pid", pid)
	return nil
}
