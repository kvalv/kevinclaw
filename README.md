# kevinclaw

A personal AI assistant that lives in Slack, powered by Claude Code. Kevin has the personality of Kevin Malone from The Office -- not the sharpest tool in the shed, but he's got heart and actually gets stuff done. He runs as a single Go binary directly on the host -- no Docker, no containers -- with a tool policy that scopes exactly which files, directories, and integrations he can access. Talks to Claude CLI as a subprocess with full session continuity, configured via `kevin.yaml` + `.env`.

## Features

- Slack: Socket Mode, thread replies, typing indicators, emoji reactions
- Claude Code CLI: subprocess per session, `--resume` for multi-turn memory
- MCP servers: Google Calendar, Home Assistant, Cron (River + Postgres), Linear
- Tool policy: owner gets full scoped access, others get read-only public paths
- Path scoping: Edit/Write/Read restricted to configured directories
- Postgres: message history, session persistence, job scheduling
- Skills: `.claude/skills/` auto-discovered (notify, etc.)

## Why not other claws?

I considered [openclaw](https://github.com/anthropics/openclaw) first: too large to understand, and running someone else's opaque agent framework on my host felt like a security risk. [nanoclaw](https://github.com/anthropics/nanoclaw) was a better fit and I forked it, but it came with baggage:

- Docker isolation: added startup latency and complexity for a single-user setup
- Multi-channel: WhatsApp, Telegram, Discord support I didn't need
- Groups: per-group containers, IPC file system, cross-channel routing
- Node.js: slower iteration, heavier runtime
- macOS-centric: credential proxy, Docker Desktop assumptions

Easier to just write my own with the core ideas in place. kevinclaw keeps session continuity, MCP tools, cron scheduling, and tool restrictions in ~2.5k lines of Go.

## Architecture

```
Slack message arrives
  │
  ├─ saved to Postgres (with user name)
  │
  ├─ System prompt (--system-prompt)
  │   Built from memory/ files:
  │   1. KEVIN.md         — personality, rules, privacy
  │   2. ## Memory         — static preamble: today's date, memory dir, conventions
  │   3. PREFERENCES.md   — durable facts about the owner (Kevin can update)
  │   4. daily/ logs       — last two days, append-only
  │
  ├─ User prompt
  │   Built from Postgres:
  │   1. Recent messages   — last 10 channel msgs or full thread (with user names)
  │   2. The actual message
  │
  └─ claude CLI subprocess
      --resume <session-id>     session continuity
      --system-prompt <prompt>  memory-backed system prompt
      --mcp-config <servers>    gcal, homeassistant, cron, linear
      --allowedTools <scoped>   per-user tool policy
      │
      └─ response → Slack thread reply
```

Key points:

- System prompt = who Kevin is + what he remembers. Rebuilt per message from files on disk.
- User prompt = what's happening now. Recent Slack context + the new message.
- Session continuity via `--resume` means Claude retains the full conversation within a thread.
- Tool policy is enforced per-user: owner gets scoped file + MCP access, others get read-only public paths.
