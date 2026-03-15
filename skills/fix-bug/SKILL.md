---
name: fix-bug
description: Fix a bug from Linear. Assesses the issue, works in a git worktree, implements the fix, creates a draft PR. Use when asked to fix a bug or given a Linear issue ID/URL.
allowed-tools: Bash, Read, Edit, Write, Glob, Grep, WebFetch, WebSearch
---

# Fix Bug

When asked to fix a bug (e.g. "fix PLA-11"), follow this workflow:

## 0. Track

Use the `bugfix` MCP tools to track your work throughout:

- `bugfix_create` — at the start, after assessment passes
- `bugfix_update` — as you progress (status changes, PR created, tokens used, errors)
- `bugfix_get` / `bugfix_list` — to check current state

Also write a run log at the path returned by `bugfix_create` (e.g. `memory/runs/PLA-11.md`). Append to it as you work — assessment, progress, findings, result.

## 1. Assess

Look up the issue in Linear. Read the description and comments. Score it on:

| Axis                | High                     | Medium                      | Low (skip)            |
| ------------------- | ------------------------ | --------------------------- | --------------------- |
| **Problem clarity** | Clear repro, obvious bug | Somewhat unclear, can infer | Vague or missing info |
| **Localizability**  | Know the file/function   | Can narrow to a module      | No idea where to look |
| **Testability**     | Existing tests cover it  | Can write a test            | No way to verify      |

Hard skip if: needs product decision, touches >5 files, spans multiple services, no repro path.

If any axis is low, tell the owner why and ask for guidance instead of proceeding.

If assessment passes, call `bugfix_create` with the issue details, confidence scores, worktree path, and branch name.

**If the issue touches `apps/`**: before writing any code, you MUST start the dev server, open the browser, navigate to the affected page, and take a "before" screenshot. This is non-negotiable — you need to SEE the bug first. Post the screenshot to the draft PR.

## 2. Set up branch + draft PR

Work in the git worktree at `~/src/main/kevin-1`:

```bash
cd ~/src/main/kevin-1
git fetch origin main
git checkout -b kevin/{issue-id}-{short-description} origin/main
```

Create the draft PR immediately — it's your live working document:

```bash
git commit --allow-empty -m "WIP: investigating {issue-id}"
git push -u origin kevin/{issue-id}-{short-description}
gh pr create --draft \
  --title "[Kevin] Fix: {issue title}" \
  --body "$(cat <<'EOF'
## {issue-id}: {issue title}

**Linear:** {issue url}
**Status:** investigating
**Confidence:** clarity={X}, localizability={X}, testability={X}

> This PR is being worked on by Kevin, Mikael's AI assistant.
> It will be marked ready for review once tests pass and the diff is clean.

## Assessment

{initial findings}
EOF
)"
```

Call `bugfix_update` with `pr_url`. All progress goes into the PR from here.

## 3. Execute

Keep changes minimal. Don't refactor surrounding code.

### Backend issues (services/, jobs/, workers/, etc.)

TDD approach:

1. **Red** — write the smallest test that reproduces the bug. Piggyback on an existing test suite if possible. Run it, confirm it fails.

   ```bash
   git add -A && git commit -m "Add failing test for {issue-id}" && git push
   gh pr comment {pr-number} --body "Added failing test: \`{test name}\`"
   ```

2. **Green** — implement the minimal fix. Run the test, confirm it passes. Run the full suite to check for regressions.
   ```bash
   git add -A && git commit -m "Fix: {issue title}\n\nCloses {issue-id}" && git push
   gh pr comment {pr-number} --body "Fix implemented, tests green."
   ```

That's it. Two commits: failing test, then fix.

### Frontend issues (apps/) — ALWAYS take screenshots

**Any bug in `apps/` is a frontend issue.** You MUST start the dev server and take before/after screenshots. This is how the reviewer validates your fix — without screenshots, the PR is incomplete.

If the bug is in `apps/`, start the dev server first:

```bash
cd ~/src/main/kevin-1
npm install
REACT_APP_DEFAULT_USER="$REACT_APP_DEFAULT_USER" \
REACT_APP_DEFAULT_PASSWORD="$REACT_APP_DEFAULT_PASSWORD" \
npm run dev:proxy &
```

Wait for the server to be ready (check `http://localhost:3000`), then:

1. **Before screenshot** — navigate to the page that shows the bug, take a screenshot with the browser MCP, save to `/tmp/{issue-id}-before.png`

   ```bash
   BEFORE_URL=$(upload-screenshot /tmp/{issue-id}-before.png {issue-id}-before)
   gh pr comment {pr-number} --body "## Before
   ![before]($BEFORE_URL)

   {description of what is wrong}"
   ```

2. **Implement the fix**

3. **After screenshot** — same page, showing the fix works

   ```bash
   AFTER_URL=$(upload-screenshot /tmp/{issue-id}-after.png {issue-id}-after)
   gh pr comment {pr-number} --body "## After
   ![after]($AFTER_URL)

   {description of what changed}"
   ```

4. Kill the dev server when done: `kill %1`

`upload-screenshot` uploads to a private GitHub release and returns an org-scoped URL.

### Progress comments

Comment on the PR at key milestones — this is your working log:

```bash
gh pr comment {pr-number} --body "Found the issue: \`handleSearch\` uses ASCII comparison..."
```

Also DM the owner via `slack_send_message` for important updates. Call `bugfix_update` with `human_update: true` after each DM.

## 4. Iterate until clean

**Backend (Go):** run scoped tests + `go vet ./...`. Fix any issues before marking ready.

**Frontend:** just push. Don't worry about translations, prettier, or CI lint — we'll fix those when wrapping up the PR.

When the diff looks good, do NOT mark as ready yet. First:

1. Wait for CI to pass: `gh pr checks {pr-number} --watch`
2. If CI fails, fix the issues and push again. Repeat until CI is green.
3. Check for bot review comments (bugbot, cursor, etc.): `gh pr view {pr-number} --comments`
4. Address all bot comments — fix issues, push, respond to each.
5. Only when CI is green AND all bot comments are addressed:
   - Edit the PR body to reflect the final state (remove WIP notes, add summary of what changed)
   - `gh pr ready {pr-number}` (removes draft)
   - DM the owner: "PR ready, who should review?"
   - Add reviewer once owner responds: `gh pr edit {pr-number} --add-reviewer {username}`

## 5. Log

Append findings and decisions to both:

- The run log at `memory/runs/{issue-id}.md` — full detail for this run
- `memory/daily/YYYY-MM-DD.md` — brief summary for daily context

## 6. Finish

Call `bugfix_update` with status `review` once the PR is marked ready for review. This signals the orchestrator to start polling for feedback.

Other final statuses:

- `done` — PR merged
- `failed` — couldn't fix it
- `stuck` — need human input, set `error` with what's blocking

## 7. Handle review feedback

The orchestrator polls every 5 minutes for bugfixes with `status = 'review'` and `pr_merged = false`. When new review comments are found, it resumes Kevin's session to address them.

When resumed for review feedback:

1. Read the PR comments/reviews: `gh pr view {pr-number} --comments`
2. Implement requested changes
3. Push, comment on what you changed
4. Increment `pr_iterations` via `bugfix_update`
5. If approved and merged: `bugfix_update` with `status: "done"`, `pr_merged: true`. Comment "nice." (in character)

## Rules

- If stuck after 2 different approaches, stop and report what you tried.
- Never loop on the same approach. Try something different or stop.
- Max 1 hour on a single issue.
- Commit messages: "Fix: {issue title}\n\nCloses {issue-id}"
- The draft PR is your working document. Keep it updated.
