package agent

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var resumeTmpl = template.Must(template.New("resume").Parse(
	`kevinclaw just restarted. These bugfixes need attention:
{{.Items}}

For each one, check the state via bugfix_get and decide:
- 'running'/'assessing': the agent died — respawn via bugfix_start or bugfix_assess
- 'review': check the PR for new comments
Handle them now.`))

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
		resumeUnfinishedBugfixes(ctx, pool, a, ownerID)

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

// resumeUnfinishedBugfixes prompts Kevin about any bugfixes that need attention after restart.
func resumeUnfinishedBugfixes(ctx context.Context, pool *pgxpool.Pool, a *Agent, ownerID string) {
	rows, err := pool.Query(ctx,
		`SELECT id, linear_issue_id, title, status
		 FROM bugfixes WHERE status IN ('running', 'assessing', 'review') ORDER BY created_at`)
	if err != nil {
		slog.Error("orchestrator: failed to query unfinished bugfixes", "err", err)
		return
	}
	defer rows.Close()

	var items []string
	for rows.Next() {
		var id int64
		var issueID, title, status string
		if err := rows.Scan(&id, &issueID, &title, &status); err != nil {
			continue
		}
		items = append(items, fmt.Sprintf("- #%d %s (%s) [%s]", id, issueID, title, status))
		slog.Info("orchestrator: unfinished bugfix", "id", id, "issue", issueID, "status", status)
	}

	if len(items) == 0 {
		return
	}

	var buf bytes.Buffer
	resumeTmpl.Execute(&buf, struct{ Items string }{strings.Join(items, "\n")})
	prompt := buf.String()

	// Use Kevin's DM session key
	key := SessionKey("orchestrator:startup")
	go func() {
		reply, err := a.HandleMessage(ctx, key, prompt, ownerID, "")
		if err != nil {
			slog.Error("orchestrator: startup resume failed", "err", err)
			return
		}
		slog.Info("orchestrator: startup resume done", "reply_len", len(reply))
	}()
}

// checkReviewPRs finds bugfixes in "review" status and nudges Kevin to re-dispatch Darryl
// so he can check for new comments and address them.
func checkReviewPRs(ctx context.Context, pool *pgxpool.Pool, a *Agent, ownerID string) int {
	rows, err := pool.Query(ctx,
		`SELECT id, linear_issue_id, title, pr_url
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
		var id int64
		var issueID, title, prURL string
		if err := rows.Scan(&id, &issueID, &title, &prURL); err != nil {
			slog.Error("orchestrator: scan failed", "err", err)
			continue
		}

		key := SessionKey(fmt.Sprintf("bugfix:%s", issueID))
		prompt := fmt.Sprintf(
			"There may be new comments on PR %s for %s (%s). "+
				"Re-dispatch Darryl via bugfix_start with id %d to check and address any comments. "+
				"If the PR has been approved and merged, update status to done with pr_merged: true.",
			prURL, issueID, title, id)

		slog.Info("orchestrator: nudging PR review", "issue", issueID, "pr", prURL)
		count++

		go func(issueID string, key SessionKey, prompt string) {
			reply, err := a.HandleMessage(ctx, key, prompt, ownerID, "")
			if err != nil {
				slog.Error("orchestrator: PR review nudge failed", "issue", issueID, "err", err)
				return
			}
			slog.Info("orchestrator: PR review nudge done", "issue", issueID, "reply_len", len(reply))
		}(issueID, key, prompt)
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
