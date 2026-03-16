package mcp

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/kvalv/kevinclaw/internal/config"
)

func TestDevServerStart(t *testing.T) {
	apps := map[string]config.AppDevConfig{
		"sample-app": {
			Port: 3000,
			SetupCmds: []string{
				"npm install",
				"npm run generate",
			},
			DevCmd: "npm run dev:proxy",
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
			desc: "known app runs setup commands then dev command",
			app:  "sample-app",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			tmpDir := t.TempDir()

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

			want := []string{"npm install", "npm run generate", "npm run dev:proxy"}
			if len(ran) != len(want) {
				t.Fatalf("expected %d commands, got %d: %v", len(want), len(ran), ran)
			}
			for i, cmd := range want {
				if ran[i] != cmd {
					t.Errorf("command %d: want %q, got %q", i, cmd, ran[i])
				}
			}
		})
	}

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
