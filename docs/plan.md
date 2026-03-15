# Home Assistant MCP — Plan

## Goal

Kevin can:

- Start/stop/dock vacuum
- Check if lights are on, turn on/off
- Read temperature sensors

## Architecture

```
internal/mcp/
└── ha.go           # HA REST API client + MCP server + toml config loader
scripts/
└── ha-export.sh    # exports entities from HA, outputs ha.toml skeleton
ha.toml             # gitignored — the actual config
ha.toml.example     # checked in — shows format
```

## ha.toml

Categories map to what Kevin can do with them. Entity IDs stay in the config, Kevin sees friendly names.

```toml
url = "https://homeassistant.example.com"
# token from env: HOMEASSISTANT_API_TOKEN

[[entities]]
id = "vacuum.roborock_s7"
name = "vacuum"
category = "vacuum"         # actions: start, stop, dock, status
description = "Living room robot vacuum"

[[entities]]
id = "light.living_room"
name = "living_room_lights"
category = "light"          # actions: turn_on, turn_off, status
description = "Main living room ceiling lights"

[[entities]]
id = "sensor.bedroom_temperature"
name = "bedroom_temp"
category = "sensor"         # actions: status (read-only)
description = "Bedroom temperature sensor (celsius)"
```

## Categories define available actions

| Category | Actions                   | HA Services                                                  |
| -------- | ------------------------- | ------------------------------------------------------------ |
| vacuum   | start, stop, dock, status | vacuum.start, vacuum.stop, vacuum.return_to_base, states API |
| light    | turn_on, turn_off, status | light.turn_on, light.turn_off, states API                    |
| sensor   | status                    | states API (read-only)                                       |

## MCP tools (2 generic tools)

- `ha_status(name?)` — get state of one entity or all. Returns friendly name + state + attributes.
- `ha_action(name, action)` — execute an action. Validates action is allowed for the category.

Example:

- `ha_status()` → "vacuum: docked, living_room_lights: on, bedroom_temp: 21.3°C"
- `ha_status("vacuum")` → "vacuum: cleaning, battery: 73%"
- `ha_action("vacuum", "start")` → "vacuum: started"
- `ha_action("living_room_lights", "turn_off")` → "living_room_lights: off"

## Export script

```bash
# ha-export.sh — fetches all entities, filters by domain, outputs toml skeleton
curl -s -H "Authorization: Bearer $TOKEN" "$URL/api/states" \
  | jq '[.[] | select(.entity_id | test("^(light|sensor|vacuum)\\."))]
        | .[] | {id: .entity_id, name: (.entity_id | split(".")[1]),
                 category: (.entity_id | split(".")[0])}'
```

User then picks which entities to keep and adds descriptions.

## Steps

1. `internal/mcp/ha.go` — API client + config loader + MCP tools
2. `ha.toml.example`
3. Wire into main.go (if ha.toml exists)
4. `scripts/ha-export.sh`
5. Block for non-owner in policy

---

# Memory, Preferences & Chat History

## How nanoclaw does it

### Message context

- Stores all messages in SQLite
- On trigger, retrieves up to 200 messages since last agent timestamp
- Formats as XML: `<message sender="Alice" time="3:45 PM">text</message>`
- Includes timezone context and thread_ts for Slack threads
- This XML is the prompt sent to the agent

### Session memory (Claude auto-memory)

- `CLAUDE_CODE_DISABLE_AUTO_MEMORY=0` — auto-memory is ON
- Each group has its own `.claude/` directory
- Claude SDK manages memory files automatically in that directory
- Sessions resume via `--resume <session_id>` with full conversation context

### Conversation archival

- Before session compaction (context too large), a `PreCompactHook` archives the transcript
- Saved to `conversations/{date}-{summary}.md` in the group workspace
- These are searchable by the agent in future sessions

### Global vs per-group

- `groups/global/CLAUDE.md` — shared personality, rules (read-only for non-main)
- `groups/{name}/` — per-group writable workspace, conversations, logs
- `data/sessions/{name}/.claude/` — per-group Claude sessions and auto-memory

## What kevinclaw has today

- Session IDs persisted in Postgres (survive restarts) ✓
- `--resume` for multi-turn continuity ✓
- All messages stored in Postgres ✓
- KEVIN.md as system prompt ✓
- Claude auto-memory is ON by default (we don't disable it) ✓

## What's missing

### 1. Chat history as context

Currently Kevin only sees the single message that triggered him. He doesn't see the last N messages in the channel/thread. This means if someone says "hey" then "can you check X", Kevin only sees "can you check X" with no context.

Options:

- Query Postgres for recent messages in the same channel/thread
- Format as XML (like nanoclaw) or plain text
- Prepend to the prompt before sending to Claude

### 2. Conversation archival

No archival before compaction. Long sessions will lose history when Claude compacts.

Options:

- Set `--append-system-prompt` with recent messages as context
- Or use Claude's built-in auto-memory (already enabled)
- Or add a conversations/ folder per session key

### 3. Preferences persistence

No explicit preferences file. Kevin's auto-memory handles some of this, but it's tied to the Claude session directory (~/.claude/).

Options:

- Let auto-memory handle it (simplest, already works)
- Add a `preferences.md` in the workdir that Kevin can read/write
