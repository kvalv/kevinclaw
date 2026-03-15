# Plan: Three-Agent Architecture

## Problem

Kevin is a single Claude session. When he's working on a bugfix (running bash commands, reading files, creating PRs), the session is blocked. Messages like "sup" queue until the current turn finishes, which can take minutes.

The `Agent` tool (Claude's built-in subagent) doesn't help — it blocks the parent session until the subagent completes. Even the assessment phase (reading Linear, scoring confidence) blocks Kevin for 30+ seconds.

## Goal

Kevin stays responsive to Slack at all times. All heavy work runs in separate processes.

## Solution: Three agents

### Kevin (the manager)

Kevin Malone. Slack-facing, always responsive. His only job is routing and managing.

When asked to fix a bug:

1. Reply "looking into it" (instant)
2. Call `bugfix_assess` MCP tool (spawns Angela)
3. Done — back to Slack

When the orchestrator nudges him about a stuck/dead agent:

1. Check state via `bugfix_get`
2. Decide: respawn, kill, or escalate to owner
3. Call `bugfix_start` to respawn if needed

Kevin never does heavy lifting. He delegates everything.

### Angela (the assessor)

Angela Martin. Separate Claude subprocess. Precise, judgemental, high standards.

Spawned by Kevin via `bugfix_assess`. Does:

1. Read the Linear issue + comments
2. Score confidence (clarity, localizability, testability)
3. Check hard gates (product decision? too many files? no repro?)
4. If passes: call `bugfix_start` to spawn Darryl with the full context
5. If fails: update bugfix status to `failed`, DM owner with why
6. Exits when done (short-lived)

### Darryl (the executor)

Darryl Philbin, warehouse guy. Separate Claude subprocess. Gets stuff done, no nonsense.

Spawned by Angela (via `bugfix_start`) or by Kevin (for respawns/review feedback). Does:

1. Work in the git worktree
2. Create draft PR immediately
3. Backend: TDD (failing test → fix → green)
4. Frontend: dev server → before screenshot → fix → after screenshot
5. Update bugfix DB as he goes
6. Send DM updates via Slack MCP
7. When done: update status to `review`

Darryl is long-lived (up to 1 hour). If he dies or gets stuck, Kevin respawns him.

### Flow

```
User: "fix PLA-11"
  → Kevin: "looking into it" (instant reply, <2s)
  → Kevin calls bugfix_assess (spawns Angela in background)
  → Kevin is free

  → Angela reads Linear issue, scores confidence
  → Angela: "all high, going for it"
  → Angela calls bugfix_start (spawns Darryl in background)
  → Angela exits

  → Darryl works in worktree: branch, PR, code, tests, screenshots
  → Darryl updates DB + DMs owner along the way
  → Darryl finishes, marks status=review

  [5 min later, orchestrator loop]
  → Go notices status=review, prompts Kevin
  → Kevin checks PR for comments
  → If feedback: Kevin spawns new Darryl round
  → If merged: Kevin marks done, comments "nice."
```

### MCP tools

**`bugfix_assess`** (new)

Spawns Angela. Takes:

- `linear_issue_id`
- `linear_issue_url`
- `title`

Does:

1. Creates bugfixes row (status=assessing)
2. Spawns Angela subprocess with assessment prompt
3. Returns immediately with run ID

**`bugfix_start`** (new)

Spawns Darryl. Takes:

- `id` — existing bugfix row ID
- `prompt` — full context for the executor
- `worktree_path`
- `branch`

Does:

1. Updates bugfixes row (status=running, worktree, branch)
2. Spawns Darryl subprocess
3. Returns immediately

**Existing tools (unchanged):**

- `bugfix_create` — manual row creation (still useful for Kevin)
- `bugfix_update` — all agents update state
- `bugfix_get` / `bugfix_list` — all agents can check state

### System prompts

**Angela (assessor):**

```
You are Angela, the assessor. You evaluate Linear issues for Kevin.
Read the issue, score confidence on three axes (clarity, localizability, testability).
Check hard gates. If it passes, call bugfix_start to spawn the executor.
If it fails, update status to failed and DM the owner explaining why.
Be precise. Miss nothing. If it doesn't meet your standards, reject it.
```

**Darryl (executor):**

```
You are Darryl, the executor. You fix bugs in a git worktree.
Follow the fix-bug workflow: branch, draft PR, TDD for backend,
screenshots for frontend. Update bugfix DB as you go. Send DM updates.
Keep it clean, keep it minimal. Warehouse efficiency.
```

### Kevin's orchestrator responsibilities

Kevin manages the team. The Go orchestrator loop nudges Kevin when action is needed:

1. **Dead agent** — Angela or Darryl process exited without updating status → prompt Kevin to check and respawn
2. **Stuck agent** — no DB update in 15 min → prompt Kevin to decide (respawn, kill, escalate)
3. **Review feedback** — PR has new comments → prompt Kevin to spawn Darryl for another round
4. **Startup resume** — unfinished bugfixes exist → prompt Kevin:
   "kevinclaw just restarted. These need attention: PLA-11 (running), PLA-22 (review).
   Check their state and spawn agents as needed."
   Kevin inspects each, decides whether to respawn Angela or Darryl.

### What the Go layer does

- Spawns subprocesses (ClaudeRunner in goroutines)
- Tracks PIDs → updates DB when process exits
- Runs the orchestrator polling loop (every 5 min)
- Wires OnEvent to dashboard SSE broker
- Provides MCP servers to all agents

### MCP servers per agent

| Server        | Kevin | Angela | Darryl |
| ------------- | ----- | ------ | ------ |
| bugfix        | yes   | yes    | yes    |
| slack         | yes   | yes    | yes    |
| linear        | yes   | yes    | no     |
| cron          | yes   | no     | no     |
| gcal          | yes   | no     | no     |
| homeassistant | yes   | no     | no     |
| debug         | yes   | no     | no     |

### Concerns

1. **Three subprocesses at peak** — Kevin + Angela + Darryl. Angela is short-lived so usually just Kevin + Darryl.
2. **One executor at a time** — one worktree. Kevin refuses a second bugfix while Darryl is working.
3. **Dead agent detection** — Go tracks PID. When process exits, check if status was properly updated. If not, mark as crashed.
4. **Session continuity** — each agent gets its own session key. Darryl can be `--resume`d for review rounds.
5. **Angela might fail to spawn Darryl** — if Angela crashes after assessment but before calling bugfix_start, the bugfix row is stuck at status=assessing. Orchestrator catches this.

### Steps

1. Add `bugfix_assess` MCP tool (spawns Angela)
2. Add `bugfix_start` MCP tool (spawns Darryl)
3. Write Angela system prompt
4. Write Darryl system prompt
5. Go: track subprocess PIDs, update DB on exit
6. Update fix-bug skill: Kevin just calls `bugfix_assess`
7. Update orchestrator loop: detect dead agents, prompt Kevin
8. Update startup resume: prompt Kevin about unfinished work
9. Test: Kevin responds to "sup" while Darryl works
