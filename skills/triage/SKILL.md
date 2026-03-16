---
name: triage
description: Triage recent Linear issues. Angela filters candidates, DMs Mikael for input, then Darryl works through them one by one.
allowed-tools: Bash
---

# Triage

When asked to triage issues (e.g. "triage recent bugs", "find issues to work on"), do this:

1. Extract the time period from the message, or default to "last month"
2. Call `triage_start` with the period (e.g. "1month", "2weeks", "3days")
3. Reply: "Angela is reviewing recent issues to find candidates."
4. Done — you're free until Angela finishes

## What happens next

- Angela pulls recent Linear issues for the given period
- She rough-filters them and writes the candidate list to today's daily log (`memory/daily/YYYY-MM-DD.md`)
- She DMs Mikael with the shortlist and asks which ones to tackle

## When Mikael picks issues

When Mikael replies with which issues to work on:

1. Save the chosen issue list to today's daily log under `## Triage queue` — include issue ID, title, and status (pending/in-progress/done/skipped)
2. Start the first issue immediately: call `bugfix_assess` for it (this spawns Angela for a full single-issue assessment, then she spawns Darryl)
3. Reply to confirm: "Starting on PLA-XX. N more in the queue."

**Do NOT re-run Angela's triage.** She already filtered. Go straight to the fix-bug flow.

## Tracking the queue

After each issue completes (PR merged, done, failed, or skipped):

1. Update the daily log — mark the issue as done/failed/skipped
2. Check if there are remaining issues in the queue
3. If yes: start the next one via `bugfix_assess`. Tell Mikael: "PLA-XX done. Moving to PLA-YY. N remaining."
4. If no: tell Mikael the queue is clear

## One at a time

Same rule as fix-bug: only one Darryl at a time. Work through the list sequentially. If Darryl is busy, wait for him to finish before starting the next.
