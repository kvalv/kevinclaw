---
name: fix-bug
description: Fix a bug from Linear. Assesses the issue, works in a git worktree, implements the fix, creates a draft PR. Use when asked to fix a bug or given a Linear issue ID/URL.
allowed-tools: Bash, Read, Edit, Write, Glob, Grep, WebFetch, WebSearch
---

# Fix Bug

When asked to fix a bug (e.g. "fix PLA-11"), follow this workflow:

## 1. Assess

Look up the issue in Linear. Read the description and comments. Score it on:

| Axis                | High                     | Medium                      | Low (skip)            |
| ------------------- | ------------------------ | --------------------------- | --------------------- |
| **Problem clarity** | Clear repro, obvious bug | Somewhat unclear, can infer | Vague or missing info |
| **Localizability**  | Know the file/function   | Can narrow to a module      | No idea where to look |
| **Testability**     | Existing tests cover it  | Can write a test            | No way to verify      |

Hard skip if: needs product decision, touches >5 files, spans multiple services, no repro path.

If any axis is low, tell the owner why and ask for guidance instead of proceeding.

## 2. Execute

Work in the git worktree at `~/src/main/kevin-1`:

```bash
cd ~/src/main/kevin-1
git fetch origin main
git checkout -b kevin/{issue-id}-{short-description} origin/main
```

Then: locate the code → reproduce the bug → implement the fix → run tests → self-review the diff.

Keep changes minimal. Don't refactor surrounding code.

### Screenshots

For frontend issues, use the browser MCP to navigate and take screenshots. Upload to Slack via the slack MCP:

1. Use `browser` MCP tools to navigate to the page and take a screenshot
2. Use `slack_upload_file` to share the screenshot with the owner

## 3. Progress updates

Send DM updates to the owner at key milestones:

- Starting: what issue, initial assessment
- Analysis: what you found
- Implementation: fix done, test results
- Completion: draft PR link
- Stuck: what you tried, where you're blocked

## 4. PR

When tests pass and diff looks clean:

- `gh pr create --draft` with title `[Kevin] Fix: {issue title}`
- Body: link to Linear issue, what changed, confidence level
- Note in body: "This PR was created by Kevin, Mikael's AI assistant"
- Tell the owner it's ready and ask who should review

## 5. Log

Append findings and decisions to `memory/daily/YYYY-MM-DD.md`. Include what you learned about the codebase — patterns, tricky areas, things that failed.

## Rules

- If stuck after 2 different approaches, stop and report what you tried.
- Never loop on the same approach. Try something different or stop.
- Max 1 hour on a single issue.
- Commit with: "Fix: {issue title}\n\nCloses {issue-id}"
