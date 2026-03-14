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
)

type recording struct {
	Hash   string   `json:"hash"`
	Prompt string   `json:"prompt"`
	Lines  []string `json:"lines"`
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
		cache[rec.Hash] = &rec
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
	cache[rec.Hash] = rec
}

// QueryClaude returns stream-json output lines for a prompt.
// Uses cached recording if available. If not and AGENT_INTEGRATION=1,
// calls the real CLI and records the result for future runs.
func QueryClaude(t *testing.T, prompt string) []string {
	t.Helper()
	ensureLoaded(t)

	hash := promptHash(prompt)

	mu.Lock()
	rec, ok := cache[hash]
	mu.Unlock()

	if ok {
		t.Logf("vcr: replaying %s", hash)
		return rec.Lines
	}

	if os.Getenv("AGENT_INTEGRATION") == "" {
		t.Skipf("vcr: no recording for %s and AGENT_INTEGRATION not set", hash)
	}

	t.Logf("vcr: recording response for: %s", prompt)
	cmd := exec.Command("claude",
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--no-session-persistence",
		prompt,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("claude CLI: %v", err)
	}

	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}

	r := &recording{Hash: hash, Prompt: prompt, Lines: lines}
	saveRecording(t, r)
	return lines
}

// ClaudeVCR returns an agent.Runner backed by recorded Claude CLI responses.
// On first run (with AGENT_INTEGRATION=1), it calls the real CLI and saves the
// response to testdata/recordings.jsonl keyed by prompt hash. On subsequent runs,
// it replays the cached response without hitting the CLI.
func ClaudeVCR(t *testing.T) func(ctx context.Context, prompt string) ([]string, error) {
	return func(_ context.Context, prompt string) ([]string, error) {
		return QueryClaude(t, prompt), nil
	}
}
