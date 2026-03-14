package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/lmittmann/tint"
)

// multiHandler fans out log records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, hh := range h.handlers {
		if hh.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, hh := range h.handlers {
		if hh.Enabled(ctx, r.Level) {
			if err := hh.Handle(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, hh := range h.handlers {
		handlers[i] = hh.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, hh := range h.handlers {
		handlers[i] = hh.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

func setupLogger() {
	_, thisFile, _, _ := runtime.Caller(0)
	logDir := filepath.Join(filepath.Dir(thisFile), "logs")
	os.MkdirAll(logDir, 0755)

	f, err := os.OpenFile(filepath.Join(logDir, "kevin.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		slog.Error("opening log file", "err", err)
		os.Exit(1)
	}

	slog.SetDefault(slog.New(&multiHandler{
		handlers: []slog.Handler{
			// Colored output to stderr
			tint.NewHandler(os.Stderr, &tint.Options{
				Level:      slog.LevelDebug,
				TimeFormat: "15:04:05",
			}),
			// JSON to log file
			slog.NewJSONHandler(f, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			}),
		},
	}))
}
