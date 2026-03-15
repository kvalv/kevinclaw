# Kevin as Bug Fixer

## Goal

Kevin acts as a junior engineer: picks up easy bugs from Linear, analyzes them, implements fixes, and produces low-risk PRs that get merged with minimal oversight. When something requires a decision or is too complex, he escalates to the owner via Slack.

## Problem Statement

Many bugs require real effort to fix, but there's a subset that are just stupid easy — off-by-one errors, missing null checks, wrong API calls, UI typos. We might as well let Kevin handle those. The ideal flow is fully automated: issue created by someone else → fixed by Kevin → approved by reviewer → merged by Kevin or human. Today the owner typically just links to Linear and says "pls fix" — this removes even that step.

Kevin should try to do the work. When he needs input, he sends a Slack message and the owner follows up. Issues that are genuinely insurmountable get ranked low in confidence and skipped.

## Architecture: Orchestrator + Executor

Two-agent design, both running as Claude Code subprocesses.

### Orchestrator

Runs on a schedule (cron) or on demand. Responsible for:

1. **Triage** — fetch open issues from Linear (filtered by team/label). For each:

   - Read the issue description + comments
   - Score on three confidence axes (see below)
   - Check hard gates (see below)
   - Skip issues that don't pass

2. **Assignment** — for issues that pass triage:

   - Allocate the git worktree (`~/src/main/kevin-1`)
   - `git fetch` then create branch: `kevin/{issue-number}-{short-title}`
   - Dispatch to an executor with the issue context

3. **Monitoring** — track executor progress:

   - Stuck detection: if executor hasn't progressed in N minutes, kill and escalate
   - Loop detection: if executor repeats same tool calls, intervene
   - Token tracking: log spend per issue for visibility (no hard cap yet)

4. **Completion** — when executor finishes:
   - Validate: tests pass? Linter clean? Diff looks reasonable?
   - Create a **draft PR** with summary, highlighting this was made by Kevin (Mikael's assistant)
   - Do NOT add reviewers yet — iterate until tests pass and diff is clean
   - Once good: notify owner via Slack, ask who should review
   - Add reviewer once owner responds

### Executor

Runs in an isolated git worktree. Responsible for:

1. **Reproduce** — for backend issues, try to reproduce locally (run tests, hit endpoint). For frontend, take a screenshot of the current state.

2. **Analyze** — find the relevant code, understand the bug, identify the fix.

3. **Implement** — write the fix, run tests, iterate until green.

4. **Self-review** — diff check: is this minimal? Does it only touch what's needed?

5. **Report** — return results to orchestrator: what changed, confidence in fix, any concerns.

6. **Log** — append findings, decisions, and review notes to `memory/daily/YYYY-MM-DD.md`. This builds recall for future sessions: patterns in the codebase, tricky areas, things that were tried and failed. The daily log is loaded into Kevin's system prompt, so yesterday's context carries forward.

7. **Progress updates** — send status to owner via Slack DM at key milestones:

   - Starting: "Working on KIT-123: button click doesn't save form"
   - Analysis: "Found the issue — `handleSubmit` missing `await` on the API call"
   - Implementation: "Fix implemented, running tests..."
   - Completion: "Tests pass. Draft PR ready: <link>"
   - Stuck: "Stuck on KIT-123 — tried X and Y, neither worked. Need input."

   Later: switch from DM to Linear comments once signal-to-noise is good.

If stuck, the executor should:

- Try a different approach (max 2 pivots)
- If still stuck after pivots, report back with what it tried and what went wrong
- Never loop forever

## Confidence Scoring

Before attempting a fix, Kevin assesses each issue on three scored axes:

| Axis                | High (auto-fix)                  | Medium (fix + flag)             | Low (skip/escalate)                      |
| ------------------- | -------------------------------- | ------------------------------- | ---------------------------------------- |
| **Problem clarity** | Clear repro steps, obvious bug   | Somewhat unclear, but can infer | Vague, contradictory, or missing info    |
| **Localizability**  | Know exactly which file/function | Can narrow to a module/area     | No idea where to look                    |
| **Testability**     | Existing tests cover the area    | Can write a test for the fix    | No tests, can't verify without manual QA |

Only attempt issues where all three axes are high or medium. If any axis is low, skip.

### Hard gates (binary skip)

These are not scored — if any apply, Kevin skips the issue immediately:

- Needs a product or design decision
- Touches >5 files or spans multiple services
- No reproduction path at all
- Labeled as architectural / refactor

## Git Worktree Setup

For now, Kevin gets one worktree:

```
~/src/main/kevin-1/
```

The worktree is:

- An isolated copy of the repo with its own branch
- Independent working directory (no file conflicts)
- Shares git objects with the main repo (disk-efficient)

The orchestrator manages the pool: assigns free worktrees to executors, reclaims them after completion or failure. Start with one, add more later if parallel work is needed.

## Stuck Detection

Three mechanisms:

1. **Timeout** — executor gets killed if no progress for N minutes. But if the agent is actively making progress, let it run. Hard max: 1 hour.

2. **Loop detection** — if the same tool call (same name + same arguments) appears >5 times in the last 15 calls, flag as stuck.

3. **Token budget** — cap total tokens per issue. If exceeded, stop and report partial progress.

On stuck:

- Executor tries one pivot (different approach)
- If still stuck after pivot, escalate to owner via Slack with context: what was tried, where it got stuck, partial analysis

## PR Process

1. Executor finishes fix → reports to orchestrator
2. Orchestrator validates (tests, lint, diff review)
3. Creates **draft PR** via `gh pr create --draft`:
   - Title: `[Kevin] Fix: {issue title}`
   - Body: issue link, what changed, confidence level, test results
   - Note: "This PR was created by Kevin, Mikael's AI assistant"
   - Labels: `kevin-fix`
4. Iterates until tests pass and diff is clean
5. Notifies owner via Slack: "draft PR ready, who should review?"
6. Adds reviewer once owner responds
7. Monitors PR for review comments
8. If reviewer requests changes → dispatch executor again with feedback
9. If approved → Kevin comments "thanks!" (in character)

## Linear Integration

Uses the Linear MCP server (already configured). Kevin needs to:

- List issues filtered by team + label (e.g. `team:platform label:bug`)
- Read issue details + comments
- Update issue status (in progress, done)
- Link PRs to issues

## What We Have Today vs What's Needed

### Have

- Linear MCP server (connected)
- Git worktree support (manual)
- Claude Code subprocess with `--resume`
- Cron scheduling (River)
- Tool policy (owner-scoped)
- `gh` CLI (authenticated)
- Slack messaging (for notifications)

### Need

- Orchestrator agent logic (triage → assign → monitor → complete)
- Executor agent logic (reproduce → analyze → implement → self-review)
- Worktree pool management
- Stuck detection
- Confidence scoring prompt
- PR creation + review monitoring
- Screenshot capability (for frontend issues)

## Research: How Others Do It

### SWE-agent

- Custom agent-computer interface optimized for code navigation/editing
- Interactive execution loop: prompt → tools → results → repeat
- 12.5% pass@1 on SWE-bench (3-5x improvement over RAG approaches)
- Single-agent, no orchestrator/executor split — but single-issue focused

### Devin

- Cloud-based parallel instances in isolated VMs
- Orchestrator dispatches sub-tasks to executor instances
- Shifted from "fully autonomous" to collaborative with human oversight
- Integration-first: Slack, GitHub webhooks for feedback loops

### Anthropic's patterns

- **Orchestrator-Workers**: lead agent coordinates, subagents execute in parallel
- **Evaluator-Optimizer**: one agent critiques, another refines (feedback loop)
- Artifact system: agents store outputs externally, pass lightweight references
- Key insight: emergent behavior in multi-agent systems is hard to predict

### Stuck detection patterns

- **Loop detection**: track sliding window of recent tool calls, flag repeats
- **Heartbeat monitoring**: distinguish "running" from "progressing"
- **Dual exit condition**: require both completion indicators AND explicit exit signal
- **Circuit breaker**: semantic error detection triggers abort

### Confidence / triage

- Every routing decision carries a confidence score (0.0-1.0)
- High (>0.9): proceed autonomously
- Medium (0.65-0.9): proceed but flag for review
- Low (<0.65): escalate immediately
- Give the LLM a way out: "Unknown" or "NeedHumanReview" as valid responses

## Next Steps

1. Design the orchestrator prompt + triage scoring
2. Design the executor prompt + stuck detection
3. Implement worktree pool management
4. Wire up the cron trigger
5. Test on a real Linear issue end-to-end
