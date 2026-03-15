package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// BugfixServer creates an MCP server for tracking bug bugfixs.
func BugfixServer(pool *pgxpool.Pool) *sdkmcp.Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "kevinclaw-bugfix", Version: "v0.0.1"}, nil)

	s.AddTool(&sdkmcp.Tool{
		Name:        "bugfix_create",
		Description: "Create a new bugfix for a Linear issue. Returns the run ID.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"linear_issue_id":  {"type": "string", "description": "e.g. PLA-11"},
				"linear_issue_url": {"type": "string"},
				"title":            {"type": "string", "description": "Issue title"},
				"worktree_path":    {"type": "string", "description": "Absolute path to git worktree"},
				"branch":           {"type": "string", "description": "Git branch name"},
				"confidence":       {"type": "object", "description": "e.g. {\"clarity\":\"high\",\"localizability\":\"medium\",\"testability\":\"high\"}"}
			},
			"required": ["linear_issue_id", "title", "worktree_path", "branch"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			IssueID    string          `json:"linear_issue_id"`
			IssueURL   string          `json:"linear_issue_url"`
			Title      string          `json:"title"`
			Worktree   string          `json:"worktree_path"`
			Branch     string          `json:"branch"`
			Confidence json.RawMessage `json:"confidence"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}

		logPath := fmt.Sprintf("memory/runs/%s.md", args.IssueID)

		var id int64
		err := pool.QueryRow(ctx,
			`INSERT INTO bugfixes (linear_issue_id, linear_issue_url, title, worktree_path, branch, confidence, log_path, status, started_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, 'running', now())
			 RETURNING id`,
			args.IssueID, args.IssueURL, args.Title, args.Worktree, args.Branch, args.Confidence, logPath,
		).Scan(&id)
		if err != nil {
			return errResult("create failed: %v", err), nil
		}
		return textResult("Run %d created. Log at %s", id, logPath), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "bugfix_update",
		Description: "Update a bugfix. Pass only the fields you want to change.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"id":           {"type": "number", "description": "Run ID"},
				"status":       {"type": "string", "description": "pending, running, stuck, done, failed, killed"},
				"pr_url":       {"type": "string"},
				"pr_merged":    {"type": "boolean"},
				"pr_iterations": {"type": "number"},
				"session_id":   {"type": "string", "description": "Claude session ID"},
				"tokens_used":  {"type": "number"},
				"error":        {"type": "string"},
				"human_update": {"type": "boolean", "description": "Set true to mark that you just messaged the owner"}
			},
			"required": ["id"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			ID           int64   `json:"id"`
			Status       *string `json:"status"`
			PRURL        *string `json:"pr_url"`
			PRMerged     *bool   `json:"pr_merged"`
			PRIterations *int    `json:"pr_iterations"`
			SessionID    *string `json:"session_id"`
			TokensUsed   *int64  `json:"tokens_used"`
			Error        *string `json:"error"`
			HumanUpdate  *bool   `json:"human_update"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}

		// Build dynamic UPDATE
		sets := []string{}
		vals := []any{}
		i := 1

		add := func(col string, val any) {
			sets = append(sets, fmt.Sprintf("%s = $%d", col, i))
			vals = append(vals, val)
			i++
		}

		if args.Status != nil {
			add("status", *args.Status)
			if *args.Status == "done" || *args.Status == "failed" {
				add("finished_at", time.Now())
			}
		}
		if args.PRURL != nil {
			add("pr_url", *args.PRURL)
		}
		if args.PRMerged != nil {
			add("pr_merged", *args.PRMerged)
			add("pr_last_checked_at", time.Now())
		}
		if args.PRIterations != nil {
			add("pr_iterations", *args.PRIterations)
		}
		if args.SessionID != nil {
			add("session_id", *args.SessionID)
		}
		if args.TokensUsed != nil {
			add("tokens_used", *args.TokensUsed)
		}
		if args.Error != nil {
			add("error", *args.Error)
		}
		if args.HumanUpdate != nil && *args.HumanUpdate {
			add("last_human_update_at", time.Now())
		}

		if len(sets) == 0 {
			return errResult("nothing to update"), nil
		}

		query := "UPDATE bugfixes SET "
		for j, s := range sets {
			if j > 0 {
				query += ", "
			}
			query += s
		}
		query += fmt.Sprintf(" WHERE id = $%d", i)
		vals = append(vals, args.ID)

		_, err := pool.Exec(ctx, query, vals...)
		if err != nil {
			return errResult("update failed: %v", err), nil
		}
		return textResult("Run %d updated.", args.ID), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "bugfix_get",
		Description: "Get details of a bugfix by ID.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"id": {"type": "number", "description": "Run ID"}
			},
			"required": ["id"]
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}

		row := pool.QueryRow(ctx,
			`SELECT id, linear_issue_id, title, status, worktree_path, branch, session_id,
			        pr_url, pr_merged, pr_iterations, pr_last_checked_at,
			        confidence, log_path, last_human_update_at, time_budget,
			        tokens_used, killed_by, killed_at, error, started_at, finished_at, created_at
			 FROM bugfixes WHERE id = $1`, args.ID)

		var r bugfixRow
		err := row.Scan(&r.ID, &r.IssueID, &r.Title, &r.Status, &r.Worktree, &r.Branch, &r.SessionID,
			&r.PRURL, &r.PRMerged, &r.PRIterations, &r.PRLastChecked,
			&r.Confidence, &r.LogPath, &r.LastHumanUpdate, &r.TimeBudget,
			&r.TokensUsed, &r.KilledBy, &r.KilledAt, &r.Error, &r.StartedAt, &r.FinishedAt, &r.CreatedAt)
		if err != nil {
			return errResult("not found: %v", err), nil
		}

		out, _ := json.MarshalIndent(r, "", "  ")
		return textResult("%s", string(out)), nil
	})

	s.AddTool(&sdkmcp.Tool{
		Name:        "bugfix_list",
		Description: "List bugfixes, optionally filtered by status.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"status": {"type": "string", "description": "Filter by status (optional)"},
				"limit":  {"type": "number", "description": "Max results (default 10)"}
			}
		}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Status string `json:"status"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult("invalid arguments: %v", err), nil
		}
		if args.Limit <= 0 {
			args.Limit = 10
		}

		var query string
		var qargs []any
		if args.Status != "" {
			query = `SELECT id, linear_issue_id, title, status, pr_url, pr_merged, tokens_used, started_at, finished_at
			         FROM bugfixes WHERE status = $1 ORDER BY created_at DESC LIMIT $2`
			qargs = []any{args.Status, args.Limit}
		} else {
			query = `SELECT id, linear_issue_id, title, status, pr_url, pr_merged, tokens_used, started_at, finished_at
			         FROM bugfixes ORDER BY created_at DESC LIMIT $1`
			qargs = []any{args.Limit}
		}

		rows, err := pool.Query(ctx, query, qargs...)
		if err != nil {
			return errResult("query failed: %v", err), nil
		}
		defer rows.Close()

		var runs []bugfixSummary
		for rows.Next() {
			var r bugfixSummary
			if err := rows.Scan(&r.ID, &r.IssueID, &r.Title, &r.Status, &r.PRURL, &r.PRMerged, &r.TokensUsed, &r.StartedAt, &r.FinishedAt); err != nil {
				continue
			}
			runs = append(runs, r)
		}

		out, _ := json.MarshalIndent(runs, "", "  ")
		return textResult("%s", string(out)), nil
	})

	return s
}

type bugfixRow struct {
	ID              int64           `json:"id"`
	IssueID         string          `json:"linear_issue_id"`
	Title           string          `json:"title"`
	Status          string          `json:"status"`
	Worktree        *string         `json:"worktree_path"`
	Branch          *string         `json:"branch"`
	SessionID       *string         `json:"session_id"`
	PRURL           *string         `json:"pr_url"`
	PRMerged        bool            `json:"pr_merged"`
	PRIterations    int             `json:"pr_iterations"`
	PRLastChecked   *time.Time      `json:"pr_last_checked_at"`
	Confidence      json.RawMessage `json:"confidence"`
	LogPath         *string         `json:"log_path"`
	LastHumanUpdate *time.Time      `json:"last_human_update_at"`
	TimeBudget      time.Duration   `json:"time_budget"`
	TokensUsed      int64           `json:"tokens_used"`
	KilledBy        *string         `json:"killed_by"`
	KilledAt        *time.Time      `json:"killed_at"`
	Error           *string         `json:"error"`
	StartedAt       *time.Time      `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at"`
	CreatedAt       time.Time       `json:"created_at"`
}

type bugfixSummary struct {
	ID         int64      `json:"id"`
	IssueID    string     `json:"linear_issue_id"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	PRURL      *string    `json:"pr_url"`
	PRMerged   bool       `json:"pr_merged"`
	TokensUsed int64      `json:"tokens_used"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}
