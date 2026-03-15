package agent

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"
)

type systemPromptData struct {
	Kevin       string
	Dir         string
	Today       string
	Preferences string
	DailyLogs   []dailyLog
}

type dailyLog struct {
	Date    string
	Content string
}

var systemPromptTmpl = template.Must(template.New("systemPrompt").Parse(
	`{{ .Kevin }}

## Memory

Today is {{ .Today }}. Your memory is stored in {{ .Dir }}.
- PREFERENCES.md — facts about the owner. Update this when you learn something new.
- daily/YYYY-MM-DD.md — append-only daily logs. Create today's if it doesn't exist.
- cron/{job-name}/ — output from scheduled jobs.
You can read and write anything in this directory.
{{- if .Preferences }}

## Preferences

{{ .Preferences }}
{{- end }}
{{- range .DailyLogs }}

## Daily Log ({{ .Date }})

{{ .Content }}
{{- end }}`))

// BuildSystemPrompt assembles the system prompt from memory files.
// today should be formatted as YYYY-MM-DD.
func BuildSystemPrompt(memoryDir, today string) string {
	data := systemPromptData{
		Dir:   memoryDir,
		Today: today,
	}

	if kevin, err := os.ReadFile(filepath.Join(memoryDir, "KEVIN.md")); err == nil {
		data.Kevin = strings.TrimSpace(string(kevin))
	}

	if prefs, err := os.ReadFile(filepath.Join(memoryDir, "PREFERENCES.md")); err == nil {
		data.Preferences = strings.TrimSpace(string(prefs))
	}

	data.DailyLogs = recentDailyLogs(memoryDir, today, 2)

	var buf bytes.Buffer
	systemPromptTmpl.Execute(&buf, data)
	return buf.String()
}

// StartDailyLogRotation creates today's daily log and spawns a goroutine
// that creates a new one at each midnight. Returns when ctx is cancelled.
func StartDailyLogRotation(ctx context.Context, memoryDir string) {
	ensureDailyLog(memoryDir)
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			select {
			case <-time.After(time.Until(next)):
				slog.Info("memory: rotating daily log")
				ensureDailyLog(memoryDir)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func ensureDailyLog(memoryDir string) {
	today := time.Now().Format(time.DateOnly)
	dailyDir := filepath.Join(memoryDir, "daily")
	if err := os.MkdirAll(dailyDir, 0755); err != nil {
		slog.Warn("memory: failed to create daily dir", "err", err)
		return
	}
	path := filepath.Join(dailyDir, today+".md")
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		slog.Warn("memory: failed to create daily log", "err", err)
	}
}

// recentDailyLogs returns the last n daily logs up to and including today, oldest first.
func recentDailyLogs(memoryDir, today string, n int) []dailyLog {
	dailyDir := filepath.Join(memoryDir, "daily")
	entries, err := os.ReadDir(dailyDir)
	if err != nil {
		return nil
	}

	todayDate, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return nil
	}

	var logs []dailyLog
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		d, err := time.Parse(time.DateOnly, name)
		if err != nil {
			continue
		}
		if d.After(todayDate) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dailyDir, e.Name()))
		if err != nil {
			continue
		}
		logs = append(logs, dailyLog{Date: name, Content: strings.TrimSpace(string(content))})
	}

	sort.Slice(logs, func(i, j int) bool { return logs[i].Date < logs[j].Date })

	if len(logs) > n {
		logs = logs[len(logs)-n:]
	}
	return logs
}
