# Memory, Preferences & Notes

## How others do it

### OpenClaw

- **SOUL.md** — identity, voice, values. Loaded first at session start.
- **MEMORY.md** — durable facts, preferences, decisions learned over time.
- **Daily logs** in `memory/YYYY-MM-DD.md` — session-specific context (today + yesterday loaded).
- **Hybrid BM25 + vector search** indexes MEMORY.md for recall without blowing context window.

Ref: [docs.openclaw.ai/concepts/memory](https://docs.openclaw.ai/concepts/memory)

Also interesting: [github.com/tobi/qmd](https://github.com/tobi/qmd)

### NanoClaw

- **Per-group CLAUDE.md** — each WhatsApp/Telegram/Discord channel gets isolated memory.
- **SQLite** for messages, groups, sessions, scheduled tasks.
- **Container isolation** — each agent runs in its own Linux container with only its CLAUDE.md mounted. Prevents cross-contamination between groups.

Ref: [github.com/qwibitai/nanoclaw](https://github.com/qwibitai/nanoclaw)

### Claude Code built-in auto-memory

- Location: `~/.claude/projects/<project>/memory/MEMORY.md`
- 200 line limit, auto-loaded at conversation start.
- On by default. Stores architecture notes, debugging insights, user preferences.

## What kevinclaw has today

- Session IDs persisted in Postgres (survive restarts)
- `--resume` for multi-turn continuity
- All messages stored in Postgres with user names
- Last 10 messages prepended as context (channel or thread)
- KEVIN.md as system prompt (personality, rules, privacy)
- Claude auto-memory on by default (we don't disable it)

## What's missing

Kevin has no durable memory he controls. Auto-memory is tied to `~/.claude/` and opaque. If the session rotates or the host changes, learned preferences vanish.

## Design: `memory/`

A `memory/` directory in the project root that Kevin can read and write via his file tools. Plain markdown. Kevin manages the contents himself — we give him the convention and write access.

```
memory/
├── .gitignore              # ignore everything except .gitignore, KEVIN.md, and PREFERENCES.md
├── KEVIN.md                # personality, rules, privacy (system prompt)
├── PREFERENCES.md          # static facts about the owner, Kevin updates over time
├── daily/
│   └── YYYY-MM-DD.md       # append-only daily logs, created at midnight (or by Kevin)
└── cron/
    └── {job-name}/
        └── {id}-YYYY-MM-DD.md   # cron job output logs (populated later)
```

### What gets loaded at conversation start

The system prompt includes:

1. **memory/KEVIN.md** — personality, rules, privacy (system prompt)
2. **memory/PREFERENCES.md** — durable facts about the owner
3. **Last two daily logs** — recent context (today + yesterday)

### PREFERENCES.md

Seeded with initial facts about the owner. Kevin reads this at session start and updates it when he learns something new. This is the "static" memory — things that don't change day-to-day.

### Daily logs

`memory/daily/YYYY-MM-DD.md` — append-only. Created at midnight or by Kevin if it doesn't exist yet. Kevin appends notable events, decisions, things he learned during the day. Today's and yesterday's logs are loaded at conversation start.

### Cron logs

`memory/cron/{job-name}/{id}-YYYY-MM-DD.md` — output from scheduled jobs. Not populated by default, will be added later.

### Why files, not database

- Claude already knows how to read/write files — no new tools needed.
- Markdown is inspectable, editable, diffable.
- Matches how OpenClaw and Claude Code's own memory work.
- Database would require a retrieval layer we don't need at this scale.

### Path scoping

`memory/` is added to Kevin's write paths in `kevin.yaml` so the owner policy allows edits. Non-owners cannot read or write memory (private data).

### KEVIN.md

Moves from project root into `memory/KEVIN.md`. Contains personality, rules, privacy, and instructions about the memory system:

- Where memory lives (`memory/`)
- That he should update PREFERENCES.md when he learns something new about the owner
- That daily logs are append-only and he should create today's if it doesn't exist
- That all of `memory/` is available for him to read and organize

### Future: BM25 search

When memory grows large, add BM25 retrieval over `memory/` so Kevin can search without loading everything into context. Not needed yet.
