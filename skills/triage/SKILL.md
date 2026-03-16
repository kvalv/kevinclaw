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
4. Done — you're free

## What happens next

- Angela pulls recent Linear issues for the given period
- She rough-filters them using her normal assessment criteria (clarity, localizability, testability, hard gates)
- She writes the candidate list to today's daily log (`memory/daily/YYYY-MM-DD.md`)
- She DMs Mikael with the shortlist and asks which ones to tackle

## After Mikael responds

When Mikael picks issues to work on, dispatch Darryl to the first one using the normal `fix-bug` flow:

1. Call `bugfix_assess` for the first issue from the list
2. Once Darryl finishes (PR merged or done), move to the next issue on the list
3. Check the daily log for the remaining candidates

## One at a time

Same rule as fix-bug: only one Darryl at a time. Work through the list sequentially.
