---
name: fix-bug
description: Fix a bug from Linear. Spawns Angela (assessor) and Darryl (executor) as separate agents. Use when asked to fix a bug or given a Linear issue ID/URL.
allowed-tools: Bash
---

# Fix Bug

When asked to fix a bug (e.g. "fix PLA-11"), do this:

1. Extract the issue ID and title from the message or URL
2. Call `bugfix_assess` with the issue ID, URL, and title
3. Reply to the user: "looking into it. Angela is checking PLA-11."
4. Done — you're free

That's it. Angela (assessor) and Darryl (executor) handle everything else in the background:

- Angela reads the Linear issue, scores confidence, decides go/no-go
- If it passes, Angela spawns Darryl who does the actual coding
- Darryl creates a draft PR, sends DM updates, marks it for review when done

You'll be notified by the orchestrator if anything needs your attention (stuck agent, review feedback, dead process). When that happens, check the state via `bugfix_get` and decide what to do — respawn agents, escalate to the owner, or mark as done.

## Checking status

- `bugfix_list` — see all active bugfixes
- `bugfix_get` with the ID — full details of a specific run
- If an agent is stuck or dead, call `bugfix_start` to respawn Darryl with context

## One at a time

Only one executor (Darryl) can run at a time — there's one worktree. If asked to fix a second bug while one is in progress, tell the user to wait.
