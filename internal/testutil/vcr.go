package testutil

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/kvalv/kevinclaw/internal/agent"
)

type recording struct {
	Test   string   `json:"test"`
	Turn   int      `json:"turn"`
	Hash   string   `json:"hash"`
	Prompt string   `json:"prompt"`
	Lines  []string `json:"lines"`
}

func recordingKey(test string, turn int, hash string) string {
	return fmt.Sprintf("%s:%d:%s", test, turn, hash)
}

var (
	mu     sync.Mutex
	cache  map[string]*recording
	loaded bool
)

func recordingsPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "recordings.jsonl")
}

func promptHash(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", h[:8])
}

func ensureLoaded(t *testing.T) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if loaded {
		return
	}
	loaded = true
	cache = make(map[string]*recording)

	f, err := os.Open(recordingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("opening recordings: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		var rec recording
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		cache[recordingKey(rec.Test, rec.Turn, rec.Hash)] = &rec
	}
}

func saveRecording(t *testing.T, rec *recording) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()

	path := recordingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("creating recordings dir: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("opening recordings for write: %v", err)
	}
	defer f.Close()
	b, _ := json.Marshal(rec)
	f.Write(b)
	f.Write([]byte("\n"))
	cache[recordingKey(rec.Test, rec.Turn, rec.Hash)] = rec
}

func queryClaude(t *testing.T, turn int, prompt, sessionID string) []string {
	t.Helper()
	ensureLoaded(t)

	hash := promptHash(prompt)
	key := recordingKey(t.Name(), turn, hash)

	mu.Lock()
	rec, ok := cache[key]
	mu.Unlock()

	if ok {
		t.Logf("vcr: replaying %s", key)
		return rec.Lines
	}

	if os.Getenv("AGENT_INTEGRATION") == "" {
		t.Skipf("vcr: no recording for %s and AGENT_INTEGRATION not set", key)
	}

	t.Logf("vcr: recording %s", key)
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
	}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	args = append(args, prompt)

	cmd := exec.Command("claude", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("claude CLI exited %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		t.Fatalf("claude CLI: %v", err)
	}

	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}

	r := &recording{Test: t.Name(), Turn: turn, Hash: hash, Prompt: prompt, Lines: lines}
	saveRecording(t, r)
	return lines
}

// ClaudeVCR returns an agent.Runner backed by recorded Claude CLI responses.
// On first run (with AGENT_INTEGRATION=1), it calls the real CLI and saves the
// response to testdata/recordings.jsonl keyed by (test name, turn index, prompt hash).
// On subsequent runs, it replays the cached response without hitting the CLI.
func ClaudeVCR(t *testing.T) agent.Runner {
	turn := 0
	return func(_ context.Context, prompt string, opts agent.RunOpts) ([]string, error) {
		lines := queryClaude(t, turn, prompt, opts.SessionID)
		turn++
		return lines, nil
	}
}
