package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kvalv/kevinclaw/internal/config"
)

func TestDevServerStart(t *testing.T) {
	apps := map[string]config.AppDevConfig{
		"sample-app": {
			Port: 0,
			SetupCmds: []string{
				"npm install",
				"npm run generate",
			},
			DevCmd: "sleep 60",
		},
	}

	cases := []struct {
		desc    string
		app     string
		wantErr string
	}{
		{
			desc:    "unknown app fails",
			app:     "nonexistent",
			wantErr: "unknown app",
		},
		{
			desc: "known app runs setup commands",
			app:  "sample-app",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			tmpDir := t.TempDir()
			os.MkdirAll(filepath.Join(tmpDir, "apps", "sample-app"), 0755)

			var ran []string
			runner := func(dir string, cmd string) error {
				ran = append(ran, cmd)
				return nil
			}

			ds := newDevServer(apps, runner)
			err := ds.Start(t.Context(), tmpDir, tc.app)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer ds.Stop()

			if len(ran) != len(apps["sample-app"].SetupCmds) {
				t.Fatalf("expected %d setup commands, got %d: %v", len(apps["sample-app"].SetupCmds), len(ran), ran)
			}
			for i, cmd := range apps["sample-app"].SetupCmds {
				if ran[i] != cmd {
					t.Errorf("command %d: want %q, got %q", i, cmd, ran[i])
				}
			}
		})
	}

	t.Run("env vars inherited by runner", func(t *testing.T) {
		t.Setenv("FOO", "bar")

		envApps := map[string]config.AppDevConfig{
			"env-app": {
				Port:      0,
				SetupCmds: []string{"echo setup"},
				DevCmd:    "sleep 60",
			},
		}

		var got string
		runner := func(dir string, cmd string) error {
			got = os.Getenv("FOO")
			return nil
		}

		tmpDir := t.TempDir()
		os.MkdirAll(filepath.Join(tmpDir, "apps", "env-app"), 0755)

		ds := newDevServer(envApps, runner)
		if err := ds.Start(t.Context(), tmpDir, "env-app"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer ds.Stop()

		if got != "bar" {
			t.Fatalf("expected FOO=bar, got %q", got)
		}
	})

	t.Run("port busy fails", func(t *testing.T) {
		ln, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatalf("failed to bind: %v", err)
		}
		defer ln.Close()
		port := ln.Addr().(*net.TCPAddr).Port

		busyApps := map[string]config.AppDevConfig{
			"busy-app": {
				Port:      port,
				SetupCmds: []string{"echo setup"},
				DevCmd:    "echo dev",
			},
		}

		ds := newDevServer(busyApps, func(dir, cmd string) error { return nil })
		err = ds.Start(t.Context(), t.TempDir(), "busy-app")
		if !errors.Is(err, ErrPortBusy) {
			t.Fatalf("expected ErrPortBusy, got %v", err)
		}
	})
}

func TestDevServerStartStop(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "apps", "test-server"), 0755)

	apps := map[string]config.AppDevConfig{
		"test-server": {
			Port:   port,
			DevCmd: fmt.Sprintf("python3 -m http.server %d", port),
		},
	}

	ds := newDevServer(apps, shellRunner)
	if err := ds.Start(t.Context(), tmpDir, "test-server"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForPort(t, port)

	if err := ds.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if !portIsFree(port) {
		t.Fatal("port still in use after Stop")
	}
}

func TestDevServerStopsOnContextCancel(t *testing.T) {
	port := freePort(t)
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "apps", "test-server"), 0755)

	apps := map[string]config.AppDevConfig{
		"test-server": {
			Port:   port,
			DevCmd: fmt.Sprintf("python3 -m http.server %d", port),
		},
	}

	ctx, cancel := context.WithCancel(t.Context())

	ds := newDevServer(apps, shellRunner)
	if err := ds.Start(ctx, tmpDir, "test-server"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitForPort(t, port)

	cancel()

	time.Sleep(500 * time.Millisecond)
	if !portIsFree(port) {
		t.Fatal("port still in use after context cancel")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func portIsFree(port int) bool {
	_, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
	return err != nil
}

func waitForPort(t *testing.T, port int) {
	t.Helper()
	addr := fmt.Sprintf("http://localhost:%d/", port)
	for range 20 {
		resp, err := http.Get(addr)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("port %d never came up", port)
}
