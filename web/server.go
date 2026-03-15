package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(
	template.New("").ParseFS(templateFS, "templates/*.html"),
)

// Server serves the Kevin dashboard.
type Server struct {
	pool    *pgxpool.Pool
	broker  *SSEBroker
	logsDir string // directory where run logs are stored
}

func NewServer(pool *pgxpool.Pool, logsDir string) *Server {
	return &Server{
		pool:    pool,
		broker:  NewSSEBroker(),
		logsDir: logsDir,
	}
}

// Broker returns the SSE broker for publishing events from outside.
func (s *Server) Broker() *SSEBroker { return s.broker }

// Handler returns the HTTP handler for the dashboard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ui", s.handleIndex)
	mux.HandleFunc("/ui/events", s.handleSSE)
	mux.HandleFunc("/ui/events/", s.handleRunSSE)
	mux.HandleFunc("/ui/bugfix/", s.handleBugfix)
	mux.HandleFunc("/ui/logs/", s.handleLogs)
	return mux
}

// --- Index page ---

type indexData struct {
	Now       time.Time
	Processes []processInfo
	Bugfixes  []bugfixInfo
}

type processInfo struct {
	PID       string
	CPU       string
	Mem       string
	Started   string
	SessionID string
}

type bugfixInfo struct {
	ID          int64
	IssueID     string
	Title       string
	Status      string
	PRURL       *string
	PRMerged    bool
	SinceUpdate string
	StartedAt   *time.Time
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := indexData{Now: time.Now()}
	data.Processes = getClaudeProcesses()

	rows, err := s.pool.Query(r.Context(),
		`SELECT id, linear_issue_id, title, status, pr_url, pr_merged,
		        COALESCE(last_human_update_at, started_at, created_at) as last_update,
		        started_at
		 FROM bugfixes ORDER BY created_at DESC LIMIT 20`)
	if err != nil {
		slog.Error("web: query failed", "err", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var b bugfixInfo
			var lastUpdate time.Time
			if err := rows.Scan(&b.ID, &b.IssueID, &b.Title, &b.Status, &b.PRURL, &b.PRMerged, &lastUpdate, &b.StartedAt); err != nil {
				continue
			}
			mins := int(time.Since(lastUpdate).Minutes())
			if mins < 1 {
				b.SinceUpdate = "just now"
			} else if mins < 60 {
				b.SinceUpdate = fmt.Sprintf("%dm ago", mins)
			} else {
				b.SinceUpdate = fmt.Sprintf("%dh%dm ago", mins/60, mins%60)
			}
			data.Bugfixes = append(data.Bugfixes, b)
		}
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.ExecuteTemplate(w, "index.html", data); err != nil {
		slog.Error("web: template error", "err", err)
	}
}

// --- Bugfix detail page ---

type bugfixDetail struct {
	ID      int64
	IssueID string
	Title   string
	Status  string
	PRURL   *string
}

func (s *Server) handleBugfix(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/ui/bugfix/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var d bugfixDetail
	err = s.pool.QueryRow(r.Context(),
		`SELECT id, linear_issue_id, title, status, pr_url FROM bugfixes WHERE id = $1`, id,
	).Scan(&d.ID, &d.IssueID, &d.Title, &d.Status, &d.PRURL)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.ExecuteTemplate(w, "bugfix.html", d); err != nil {
		slog.Error("web: template error", "err", err)
	}
}

// handleLogs returns the raw log lines for a bugfix, reading all log files matching the issue ID.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/ui/logs/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Get issue ID from DB
	var issueID string
	err = s.pool.QueryRow(r.Context(),
		`SELECT linear_issue_id FROM bugfixes WHERE id = $1`, id,
	).Scan(&issueID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Read all log files matching this issue (angela + darryl)
	w.Header().Set("Content-Type", "application/json")
	entries, _ := os.ReadDir(s.logsDir)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), issueID+"-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.logsDir, e.Name()))
		if err != nil {
			continue
		}
		w.Write(data)
	}
}

// --- SSE endpoints ---

// handleSSE streams all events plus periodic state updates.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.broker.Subscribe(0)
	defer s.broker.Unsubscribe(ch)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Send initial state immediately
	s.sendState(w, flusher, r.Context())

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			s.sendState(w, flusher, r.Context())
		case <-r.Context().Done():
			return
		}
	}
}

type dashboardState struct {
	Processes []processJSON `json:"processes"`
	Bugfixes  []bugfixJSON  `json:"bugfixes"`
}

type processJSON struct {
	PID       string `json:"pid"`
	CPU       string `json:"cpu"`
	Mem       string `json:"mem"`
	Started   string `json:"started"`
	SessionID string `json:"session_id,omitempty"`
}

type bugfixJSON struct {
	ID          int64   `json:"id"`
	IssueID     string  `json:"issue_id"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	PRURL       *string `json:"pr_url"`
	PRMerged    bool    `json:"pr_merged"`
	SinceUpdate string  `json:"since_update"`
	StartedAt   string  `json:"started_at,omitempty"`
}

func (s *Server) sendState(w http.ResponseWriter, flusher http.Flusher, ctx context.Context) {
	state := dashboardState{}

	for _, p := range getClaudeProcesses() {
		state.Processes = append(state.Processes, processJSON{
			PID: p.PID, CPU: p.CPU, Mem: p.Mem, Started: p.Started, SessionID: p.SessionID,
		})
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, linear_issue_id, title, status, pr_url, pr_merged,
		        COALESCE(last_human_update_at, started_at, created_at) as last_update,
		        started_at
		 FROM bugfixes ORDER BY created_at DESC LIMIT 20`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var b bugfixJSON
			var lastUpdate time.Time
			var startedAt *time.Time
			if err := rows.Scan(&b.ID, &b.IssueID, &b.Title, &b.Status, &b.PRURL, &b.PRMerged, &lastUpdate, &startedAt); err != nil {
				continue
			}
			mins := int(time.Since(lastUpdate).Minutes())
			if mins < 1 {
				b.SinceUpdate = "just now"
			} else if mins < 60 {
				b.SinceUpdate = fmt.Sprintf("%dm ago", mins)
			} else {
				b.SinceUpdate = fmt.Sprintf("%dh%dm ago", mins/60, mins%60)
			}
			if startedAt != nil {
				b.StartedAt = startedAt.Format("15:04")
			}
			state.Bugfixes = append(state.Bugfixes, b)
		}
	}

	data, _ := json.Marshal(state)
	fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
	flusher.Flush()
}

// handleRunSSE streams events for a specific run ID.
func (s *Server) handleRunSSE(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/ui/events/")
	runID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.broker.Subscribe(runID)
	defer s.broker.Unsubscribe(ch)

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// --- Process listing ---

var sessionRe = regexp.MustCompile(`--resume\s+(\S+)`)

func getClaudeProcesses() []processInfo {
	out, err := exec.Command("ps", "aux").Output()
	if err != nil {
		return nil
	}

	var procs []processInfo
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "claude") || !strings.Contains(line, "--system-prompt") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}

		p := processInfo{
			PID:     fields[1],
			CPU:     fields[2],
			Mem:     fields[3],
			Started: fields[8],
		}

		if m := sessionRe.FindStringSubmatch(line); len(m) > 1 {
			sid := m[1]
			if len(sid) > 12 {
				sid = sid[:12] + "..."
			}
			p.SessionID = sid
		}

		procs = append(procs, p)
	}
	return procs
}

// --- SSE Broker ---

type sub struct {
	ch    chan string
	runID int64 // 0 = all events
}

// SSEBroker fans out events to subscribers, optionally filtered by run ID.
type SSEBroker struct {
	mu   sync.RWMutex
	subs map[chan string]*sub
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{subs: make(map[chan string]*sub)}
}

// Subscribe returns a channel that receives events.
// If runID is 0, receives all events. Otherwise only events for that run.
func (b *SSEBroker) Subscribe(runID int64) chan string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan string, 50)
	b.subs[ch] = &sub{ch: ch, runID: runID}
	return ch
}

func (b *SSEBroker) Unsubscribe(ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, ch)
	close(ch)
}

// Publish sends an event to matching subscribers.
// The role (e.g. "angela", "darryl", "kevin") is prepended as a tab-separated prefix.
func (b *SSEBroker) Publish(runID int64, role, msg string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	line := role + "\t" + msg
	for _, s := range b.subs {
		if s.runID != 0 && s.runID != runID {
			continue
		}
		select {
		case s.ch <- line:
		default:
		}
	}
}

// ListenForLogs wires structured logging into the live event stream (future).
func (b *SSEBroker) ListenForLogs(ctx context.Context) {}
