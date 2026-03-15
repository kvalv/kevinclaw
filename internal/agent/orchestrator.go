package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type bugfixRow struct {
	ID            int64
	IssueID       string
	Title         string
	Status        string
	PRURL         *string
	SessionID     *string
	LastHumanAt   *time.Time
	StartedAt     *time.Time
	PRLastChecked *time.Time
}

// StartOrchestrator runs a background loop that polls bugfixes every interval.
// It checks for:
// - PRs in "review" status that may have new comments
// - Runs in "running" status that may be stuck (no update in stuckAfter)
// On startup, it resumes any bugfixes that were left in "running" state.
func StartOrchestrator(ctx context.Context, pool *pgxpool.Pool, a *Agent, ownerID string, interval, stuckAfter time.Duration) {
	go func() {
		resumeRunningBugfixes(ctx, pool, a, ownerID)

		// Wait a bit before first poll
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
			return
		}

		slog.Info("orchestrator: started", "interval", interval, "stuck_after", stuckAfter)

		for {
			reviewCount := checkReviewPRs(ctx, pool, a, ownerID)
			stuckCount := checkStuckRuns(ctx, pool, a, ownerID, stuckAfter)
			slog.Info("orchestrator: poll complete", "review_prs", reviewCount, "stuck_runs", stuckCount)

			select {
			case <-time.After(interval):
			case <-ctx.Done():
				return
			}
		}
	}()
}

// resumeRunningBugfixes picks up any bugfixes left in "running" state from a previous run.
func resumeRunningBugfixes(ctx context.Context, pool *pgxpool.Pool, a *Agent, ownerID string) {
	rows, err := pool.Query(ctx,
		`SELECT id, linear_issue_id, title, pr_url, session_id
		 FROM bugfixes WHERE status = 'running' ORDER BY created_at`)
	if err != nil {
		slog.Error("orchestrator: failed to query running bugfixes", "err", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var b bugfixRow
		if err := rows.Scan(&b.ID, &b.IssueID, &b.Title, &b.PRURL, &b.SessionID); err != nil {
			slog.Error("orchestrator: scan failed", "err", err)
			continue
		}

		slog.Info("orchestrator: resuming bugfix from previous run", "id", b.ID, "issue", b.IssueID, "title", b.Title)

		key := SessionKey(fmt.Sprintf("bugfix:%s", b.IssueID))

		prompt := fmt.Sprintf(
			"You were working on %s (%s) but the process was restarted. "+
				"Pick up where you left off — check the bugfix state (id %d), "+
				"review your progress in the run log and PR, and continue.",
			b.IssueID, b.Title, b.ID)

		go func(issueID string, id int64, key SessionKey, prompt string) {
			reply, err := a.HandleMessage(ctx, key, prompt, ownerID, "", WithRunID(id))
			if err != nil {
				slog.Error("orchestrator: resume failed", "issue", issueID, "err", err)
				return
			}
			slog.Info("orchestrator: resume done", "issue", issueID, "reply_len", len(reply))
		}(b.IssueID, b.ID, key, prompt)
	}
}

// checkReviewPRs finds bugfixes in "review" status and prompts Kevin to check for new feedback.
func checkReviewPRs(ctx context.Context, pool *pgxpool.Pool, a *Agent, ownerID string) int {
	rows, err := pool.Query(ctx,
		`SELECT id, linear_issue_id, title, pr_url, session_id, pr_last_checked_at
		 FROM bugfixes
		 WHERE status = 'review' AND pr_merged = false AND pr_url IS NOT NULL
		 ORDER BY created_at`)
	if err != nil {
		slog.Error("orchestrator: failed to query review PRs", "err", err)
		return 0
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var b bugfixRow
		if err := rows.Scan(&b.ID, &b.IssueID, &b.Title, &b.PRURL, &b.SessionID, &b.PRLastChecked); err != nil {
			slog.Error("orchestrator: scan failed", "err", err)
			continue
		}

		// Use the bugfix session key so Kevin resumes the same conversation
		key := SessionKey(fmt.Sprintf("bugfix:%s", b.IssueID))

		prompt := fmt.Sprintf(
			"Check PR %s for new review comments on %s (%s). "+
				"If there are changes requested, address them — push fixes, comment on what you changed, "+
				"and update the bugfix via bugfix_update. "+
				"If the PR has been approved and merged, update status to done with pr_merged: true. "+
				"If no new comments, just update pr_last_checked_at via bugfix_update with id %d.",
			*b.PRURL, b.IssueID, b.Title, b.ID)

		slog.Info("orchestrator: checking PR", "issue", b.IssueID, "pr", *b.PRURL)
		count++

		go func(issueID string, key SessionKey, prompt string) {
			reply, err := a.HandleMessage(ctx, key, prompt, ownerID, "")
			if err != nil {
				slog.Error("orchestrator: PR check failed", "issue", issueID, "err", err)
				return
			}
			slog.Info("orchestrator: PR check done", "issue", issueID, "reply_len", len(reply))
		}(b.IssueID, key, prompt)
	}
	return count
}

// checkStuckRuns finds bugfixes in "running" status with no human update for stuckAfter duration.
func checkStuckRuns(ctx context.Context, pool *pgxpool.Pool, a *Agent, ownerID string, stuckAfter time.Duration) int {
	cutoff := time.Now().Add(-stuckAfter)

	rows, err := pool.Query(ctx,
		`SELECT id, linear_issue_id, title, session_id, last_human_update_at, started_at
		 FROM bugfixes
		 WHERE status = 'running'
		   AND COALESCE(last_human_update_at, started_at, created_at) < $1
		 ORDER BY created_at`, cutoff)
	if err != nil {
		slog.Error("orchestrator: failed to query stuck runs", "err", err)
		return 0
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var b bugfixRow
		if err := rows.Scan(&b.ID, &b.IssueID, &b.Title, &b.SessionID, &b.LastHumanAt, &b.StartedAt); err != nil {
			slog.Error("orchestrator: scan failed", "err", err)
			continue
		}

		key := SessionKey(fmt.Sprintf("bugfix:%s", b.IssueID))

		prompt := fmt.Sprintf(
			"You're working on %s (%s) but haven't sent an update in a while. "+
				"What's your status? If you're stuck, update the bugfix (id %d) with status 'stuck' and describe what's blocking you. "+
				"If you're making progress, send a brief update and continue.",
			b.IssueID, b.Title, b.ID)

		slog.Info("orchestrator: nudging stuck run", "issue", b.IssueID, "id", b.ID)
		count++

		go func(issueID string, key SessionKey, prompt string) {
			reply, err := a.HandleMessage(ctx, key, prompt, ownerID, "")
			if err != nil {
				slog.Error("orchestrator: nudge failed", "issue", issueID, "err", err)
				return
			}
			slog.Info("orchestrator: nudge done", "issue", issueID, "reply_len", len(reply))
		}(b.IssueID, key, prompt)
	}
	return count
}
