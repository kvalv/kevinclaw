# kevinclaw — Feature Set

Go server. Single binary. No Docker. Runs on host. Fast and lean.

## Technologies

- **Go** — single statically-linked binary
- **Postgres** (via `pgx`) — sole external service. Used for everything:
  - Application data (messages, groups, sessions)
  - Job scheduling / cron (via River — see below)
  - Pub/sub (via `LISTEN`/`NOTIFY`)
- **River** ([github.com/riverqueue/river](https://pkg.go.dev/github.com/riverqueue/river)) — Postgres-backed job queue, built on pgx. Supports cron scheduling, retries, unique jobs. No Redis needed.
- **Slack SDK** — Socket Mode (no public URL / webhook needed)
- **Claude Code CLI** — invoked as subprocess per agent session

## Slack Integration

Uses `slack-go/slack` with Socket Mode. No public URL needed.

### `internal/slack/` — our connection to Slack

Receives events, sends responses. The Go server calls this directly.

**Inbound events we handle:**

- `message` / `app_mention` — someone talks to or mentions Kevin
- `message` (in thread) — reply in a thread Kevin is participating in
- `reaction_added` — someone reacts to a message (context signal)
- `file_share` — someone shares a file (could be code, screenshot, etc.)

**Outbound actions we perform:**

- `SendMessage(channel, text, threadTS)` — post or reply
- `SetTyping(channel)` — typing indicator while agent is working
- `AddReaction(channel, timestamp, emoji)` — acknowledge receipt (e.g. :eyes:)
- `UploadFile(channel, file)` — share files back

**High-level interface (testable):**
s
```go
type SlackClient interface {
    // Inbound — returns a channel of events we care about
    Events() <-chan Event

    // Outbound
    Send(ctx context.Context, msg OutgoingMessage) error
    React(ctx context.Context, channel, timestamp, emoji string) error
    SetTyping(ctx context.Context, channel string) error
}
```

`Event` is our own type — normalized from Slack's event zoo. Keeps agent/ decoupled from Slack SDK types.

### Slack MCP server — tools exposed _to the agent_

Things Kevin might want to do proactively (not in response to a message):

- `read_thread(channel, thread_ts)` — read full thread context
- `search_messages(query)` — search workspace history
- `list_channels()` — discover channels
- `read_channel_history(channel, limit)` — recent messages in a channel
- `send_message(channel, text, thread_ts?)` — post to any channel/thread
- `add_reaction(channel, timestamp, emoji)` — react to messages

The split: **`internal/slack/`** is infrastructure (event loop, connection). **Slack MCP** is the agent's hands — tools it can call via MCP to interact with Slack beyond just replying to the current message.

## Agent Execution

Invokes `claude` CLI as a subprocess on the host. No Docker.

### How nanoclaw does it (for reference)

- Spawns Docker container per group, pipes JSON to stdin
- Container runs Claude SDK `query()` internally
- Output wrapped in sentinel markers, parsed by host
- IPC via filesystem (host ↔ agent exchange JSON files)
- Session IDs stored in DB, passed on next invocation to resume

### How we do it

Direct subprocess via `claude` CLI with streaming JSON:

```
claude --print \
  --output-format stream-json \
  --input-format stream-json \
  --resume <session-id> \
  --session-id <uuid> \
  --mcp-config mcp.json \
  --system-prompt "You are Kevin..." \
  --dangerously-skip-permissions \
  "the user prompt here"
```

### Session routing

One subprocess per conversation context (thread or DM):

```
Slack thread_ts=1234 → Session A (running) → pipe message to stdin
Slack thread_ts=5678 → Session B (idle)    → spawn new subprocess, resume session
Slack DM from user   → Session C           → ...
```

`agent/` manages a map of active sessions. On incoming message:

1. Look up session by (channel, thread_ts) key
2. If subprocess alive → write to its stdin (stream-json input)
3. If no subprocess → spawn `claude` with `--resume <sessionID>` if we have one
4. Parse stream-json stdout → route response back to slack

### Lifecycle

```
message arrives
  → agent.HandleMessage(channel, threadTS, text)
  → lookup/spawn session subprocess
  → write prompt to stdin (stream-json)
  → read stream-json from stdout
     → on assistant text → slack.SendMessage(channel, text, threadTS)
     → on tool use → (claude handles internally)
     → on end → keep subprocess alive (idle timeout)
  → idle timeout (e.g. 5min) → kill subprocess, save session ID to DB
```

### MCP servers

Passed via `--mcp-config mcp.json`:

```json
{
  "mcpServers": {
    "slack": { "command": "kevinclaw-mcp-slack", "args": [...] },
    "cron":  { "command": "kevinclaw-mcp-cron", "args": [...] }
  }
}
```

Host spawns MCP servers as stdio subprocesses. Claude CLI connects to them.
Agent can call `slack.read_thread`, `cron.schedule_task`, etc.

### `internal/agent/` — suggested structure

```go
// ---- Keys & Config ----

// SessionKey identifies a conversation context.
// For threaded messages: (channel, thread_ts). For top-level DMs: (channel, "").
type SessionKey struct {
    Channel  string
    ThreadTS string
}

type Config struct {
    IdleTimeout   time.Duration // kill subprocess after inactivity (default 5m)
    SystemPrompt  string        // system prompt for Kevin
    MCPConfigPath string        // path to mcp.json
    WorkDir       string        // cwd for claude subprocess
    MaxSessions   int           // concurrent session limit
    AllowedPaths  []string      // paths agent can edit (e.g. ~/scripts, ~/src/main/a)
}

// ---- Stream JSON types (from claude CLI --output-format stream-json) ----

// InitEvent is the first event: session_id, tools, model, etc.
type InitEvent struct {
    Type      string `json:"type"`       // "system"
    Subtype   string `json:"subtype"`    // "init"
    SessionID string `json:"session_id"`
    Model     string `json:"model"`
}

// AssistantEvent contains the model's response message.
type AssistantEvent struct {
    Type      string          `json:"type"` // "assistant"
    Message   AssistantMessage `json:"message"`
    SessionID string          `json:"session_id"`
}

type AssistantMessage struct {
    Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
    Type string `json:"type"` // "text", "tool_use", "tool_result"
    Text string `json:"text,omitempty"`
}

// ResultEvent is the final event: success/error, cost, session_id.
type ResultEvent struct {
    Type      string  `json:"type"`    // "result"
    Subtype   string  `json:"subtype"` // "success" | "error"
    Result    string  `json:"result"`
    SessionID string  `json:"session_id"`
    CostUSD   float64 `json:"total_cost_usd"`
}

// ---- Callback interface (how agent talks back to the caller) ----

// ResponseHandler is called by the agent when it has output.
// Keeps agent/ decoupled from slack/.
type ResponseHandler interface {
    OnText(ctx context.Context, key SessionKey, text string) error
    OnError(ctx context.Context, key SessionKey, err error)
}

// ---- Core agent ----

// Agent manages claude subprocess sessions.
type Agent struct { /* config, sessions map, mu sync.Mutex */ }

func New(cfg Config) *Agent

// HandleMessage routes a message to the right session.
// Spawns a new subprocess if needed, or pipes to existing one.
func (a *Agent) HandleMessage(ctx context.Context, key SessionKey, text string, handler ResponseHandler) error

// Shutdown gracefully kills all active sessions.
func (a *Agent) Shutdown(ctx context.Context) error

// ---- Internal session ----

// session represents one active claude subprocess.
type session struct { /* cmd *exec.Cmd, stdin io.Writer, sessionID string, idleTimer, mu */ }

// spawn starts a new claude CLI subprocess.
func (a *Agent) spawn(ctx context.Context, key SessionKey, prompt string, handler ResponseHandler) (*session, error)

// send writes a follow-up message to an active session's stdin (stream-json input).
func (s *session) send(text string) error

// readLoop reads stream-json from stdout, calls handler.OnText for assistant messages.
func (s *session) readLoop(ctx context.Context, key SessionKey, handler ResponseHandler)

// kill terminates the subprocess.
func (s *session) kill() error
```

Key design decisions:

- **`ResponseHandler` interface** — agent doesn't import slack. Caller (main.go) provides a handler that forwards to slack. Easy to mock in tests.
- **`SessionKey`** — (channel, thread_ts) tuple. Top-level messages use empty thread_ts.
- **Stream-json parsing** — line-by-line JSON from stdout. We only care about `assistant` events (text to relay) and `result` (session done / error).
- **`send()` on active session** — for follow-up messages in the same thread while Claude is still running. Uses `--input-format stream-json` on stdin.
- **`AllowedPaths`** — translated to CLI args. Agent can read anywhere but only edit within these paths:
  ```
  claude --print \
    --dangerously-skip-permissions \
    --allowed-tools "Edit(/home/user/scripts/**) Write(/home/user/scripts/**) \
                     Edit(/home/user/src/main/a/**) Write(/home/user/src/main/a/**) \
                     Bash Read Glob Grep WebSearch WebFetch" \
    --add-dir /home/user/scripts \
    --add-dir /home/user/src/main/a \
    ...
  ```

## Skills

- Extensible skill/tool system
- Skills registered and discoverable by the agent
- Global CLAUDE.md memory file

## Cron / Task Scheduling

- **River** for all scheduling — cron expressions, intervals, one-shot
- Postgres-native: job state, locks, retries all in the same DB
- Task run logging (status, errors, attempts)
- Agent can register/modify scheduled tasks at runtime

## Database

- **Postgres** via `pgx`
- Tables: messages, groups, sessions, scheduled tasks, task logs
- Message storage with sender, timestamp, thread context

## MCP Support

- Expose MCP servers to the agent (stdio subprocess pattern)
- Built-in MCP servers: IPC (cross-channel messaging, task scheduling)
- Pluggable external MCP servers (Linear, Google Calendar, etc.)

## Message Handling

- Store all messages, trigger on mention
- Thread-aware replies
- Sender allowlist / access control

## Package Structure

```
kevinclaw/
├── main.go
├── internal/
│   ├── config/     # .env loading, app config struct
│   ├── db/         # pgx pool, migrations, queries
│   ├── slack/      # Slack Socket Mode — receive/send only
│   ├── agent/      # session routing, Claude subprocess lifecycle
│   ├── cron/       # scheduled tasks via River
│   └── mcp/        # MCP server management
├── migrations/
└── docs/
```

`main.go` wires everything. `slack/` receives a message, hands it to `agent/`, which routes to the right session (by thread/channel) — pipes to existing subprocess or spawns new one. Response flows back through `slack/`.

## Credential Management

- Secrets in `.env` file, loaded at startup
- Per-integration credential config (API keys, OAuth tokens, DSN)

## Features to Consider (from nanoclaw)

- **Tool restriction per sender** — public channel users get limited tool set, owner gets full access
- **IPC file system** — request/response pattern between host and agent subprocess
- **Session continuity** — reuse Claude session IDs across invocations
- **Message batching** — collect messages while agent is busy, deliver on next invocation
- **Idle timeout** — auto-stop agent after inactivity
- **Output sentinels** — structured output parsing from subprocess
- **Mount/path security** — allowlist for any filesystem access given to agent
- **Retry with backoff** — on agent failures
