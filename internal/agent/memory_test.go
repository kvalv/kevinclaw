package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func memoryPreamble(dir, today string) string {
	var buf bytes.Buffer
	systemPromptTmpl.Execute(&buf, systemPromptData{Dir: dir, Today: today})
	full := buf.String()
	start := strings.Index(full, "## Memory")
	if start == -1 {
		return ""
	}
	return strings.TrimSpace(full[start:])
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "KEVIN.md"), "You are Kevin.")
	writeFile(t, filepath.Join(dir, "PREFERENCES.md"), "Owner likes chili.")
	writeFile(t, filepath.Join(dir, "daily", "2026-03-14.md"), "deployed v2.")
	writeFile(t, filepath.Join(dir, "daily", "2026-03-15.md"), "fixed bug.")

	got := BuildSystemPrompt(dir, "2026-03-15")

	want := fmt.Sprintf(`You are Kevin.

%s

## Preferences

Owner likes chili.

## Daily Log (2026-03-14)

deployed v2.

## Daily Log (2026-03-15)

fixed bug.`, memoryPreamble(dir, "2026-03-15"))

	if got != want {
		t.Errorf("prompt mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBuildSystemPrompt_MissingFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "KEVIN.md"), "You are Kevin.")

	got := BuildSystemPrompt(dir, "2026-03-15")

	want := fmt.Sprintf(`You are Kevin.

%s`, memoryPreamble(dir, "2026-03-15"))

	if got != want {
		t.Errorf("prompt mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBuildSystemPrompt_OnlyTodayLog(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "KEVIN.md"), "You are Kevin.")
	writeFile(t, filepath.Join(dir, "daily", "2026-03-15.md"), "stuff happened.")

	got := BuildSystemPrompt(dir, "2026-03-15")

	want := fmt.Sprintf(`You are Kevin.

%s

## Daily Log (2026-03-15)

stuff happened.`, memoryPreamble(dir, "2026-03-15"))

	if got != want {
		t.Errorf("prompt mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBuildSystemPrompt_PicksLastTwoDays(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "KEVIN.md"), "You are Kevin.")
	writeFile(t, filepath.Join(dir, "daily", "2026-03-13.md"), "Three days ago.")
	writeFile(t, filepath.Join(dir, "daily", "2026-03-14.md"), "Two days ago.")
	writeFile(t, filepath.Join(dir, "daily", "2026-03-15.md"), "Today.")

	got := BuildSystemPrompt(dir, "2026-03-15")

	want := fmt.Sprintf(`You are Kevin.

%s

## Daily Log (2026-03-14)

Two days ago.

## Daily Log (2026-03-15)

Today.`, memoryPreamble(dir, "2026-03-15"))

	if got != want {
		t.Errorf("prompt mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
