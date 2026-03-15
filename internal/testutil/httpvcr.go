package testutil

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

type httpRecording struct {
	Test     string `json:"test"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	BodyHash string `json:"body_hash,omitempty"`
	Status   int    `json:"status"`
	Response string `json:"response"`
}

func httpRecordingKey(test, method, path, bodyHash string) string {
	return fmt.Sprintf("%s:%s:%s:%s", test, method, path, bodyHash)
}

var (
	httpMu     sync.Mutex
	httpCache  map[string]*httpRecording
	httpLoaded bool
)

func httpRecordingsPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "http_recordings.jsonl")
}

func ensureHTTPLoaded(t *testing.T) {
	t.Helper()
	httpMu.Lock()
	defer httpMu.Unlock()
	if httpLoaded {
		return
	}
	httpLoaded = true
	httpCache = make(map[string]*httpRecording)

	f, err := os.Open(httpRecordingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("opening http recordings: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		var rec httpRecording
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		httpCache[httpRecordingKey(rec.Test, rec.Method, rec.Path, rec.BodyHash)] = &rec
	}
}

func saveHTTPRecording(t *testing.T, rec *httpRecording) {
	t.Helper()
	httpMu.Lock()
	defer httpMu.Unlock()

	path := httpRecordingsPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("opening http recordings for write: %v", err)
	}
	defer f.Close()
	b, _ := json.Marshal(rec)
	f.Write(b)
	f.Write([]byte("\n"))
	httpCache[httpRecordingKey(rec.Test, rec.Method, rec.Path, rec.BodyHash)] = rec
}

func bodyHash(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	h := sha256.Sum256(body)
	return fmt.Sprintf("%x", h[:8])
}

// HTTPVCR returns an httptest.Server that replays cached responses.
// On cache miss with HA_INTEGRATION=1, proxies to realURL and records the response.
// On cache miss without the flag, the test is skipped.
func HTTPVCR(t *testing.T, realURL string) *httptest.Server {
	t.Helper()
	ensureHTTPLoaded(t)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hash := bodyHash(body)
		key := httpRecordingKey(t.Name(), r.Method, r.URL.Path, hash)

		httpMu.Lock()
		rec, ok := httpCache[key]
		httpMu.Unlock()

		if ok {
			w.WriteHeader(rec.Status)
			w.Write([]byte(rec.Response))
			return
		}

		if os.Getenv("HA_INTEGRATION") == "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error": "no recording and HA_INTEGRATION not set"}`))
			return
		}

		// Proxy to real server and record
		proxyReq, _ := http.NewRequest(r.Method, realURL+r.URL.Path, bytes.NewReader(body))
		for k, v := range r.Header {
			proxyReq.Header[k] = v
		}
		resp, err := http.DefaultClient.Do(proxyReq)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(err.Error()))
			return
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		saveHTTPRecording(t, &httpRecording{
			Test:     t.Name(),
			Method:   r.Method,
			Path:     r.URL.Path,
			BodyHash: hash,
			Status:   resp.StatusCode,
			Response: string(respBody),
		})

		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	}))
}
