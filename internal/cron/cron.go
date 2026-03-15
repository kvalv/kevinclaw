package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Scheduler manages scheduled tasks via River.
type Scheduler struct {
	client *river.Client[pgx.Tx]
	pool   *pgxpool.Pool
}

// New creates a Scheduler, runs River migrations, and starts the client.
func New(ctx context.Context, pool *pgxpool.Pool, handler PromptHandler) (*Scheduler, error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &promptWorker{handler: handler})

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 5},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("creating river client: %w", err)
	}

	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("starting river client: %w", err)
	}
	slog.Info("cron: river started")

	return &Scheduler{client: client, pool: pool}, nil
}

// Schedule inserts a one-off prompt job.
func (s *Scheduler) Schedule(ctx context.Context, args PromptJobArgs) error {
	_, err := s.client.Insert(ctx, args, nil)
	if err != nil {
		return fmt.Errorf("scheduling job: %w", err)
	}
	slog.Info("cron: job scheduled", "session_key", args.SessionKey, "prompt_len", len(args.Prompt))
	return nil
}

// JobInfo is a summary of a scheduled job.
type JobInfo struct {
	ID         int64  `json:"id"`
	State      string `json:"state"`
	SessionKey string `json:"session_key"`
	Prompt     string `json:"prompt"`
}

// List returns pending/scheduled/running jobs.
func (s *Scheduler) List(ctx context.Context) ([]JobInfo, error) {
	params := river.NewJobListParams().First(50)
	result, err := s.client.JobList(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}

	var jobs []JobInfo
	for _, row := range result.Jobs {
		var args PromptJobArgs
		if err := json.Unmarshal(row.EncodedArgs, &args); err != nil {
			continue
		}
		jobs = append(jobs, JobInfo{
			ID:         row.ID,
			State:      string(row.State),
			SessionKey: args.SessionKey,
			Prompt:     args.Prompt,
		})
	}
	return jobs, nil
}

// Cancel cancels a job by ID.
func (s *Scheduler) Cancel(ctx context.Context, jobID int64) error {
	_, err := s.client.JobCancel(ctx, jobID)
	if err != nil {
		return fmt.Errorf("cancelling job %d: %w", jobID, err)
	}
	slog.Info("cron: job cancelled", "job_id", jobID)
	return nil
}

// Stop gracefully shuts down the River client.
func (s *Scheduler) Stop(ctx context.Context) error {
	return s.client.Stop(ctx)
}

// PromptHandler is called when a scheduled prompt job fires.
type PromptHandler func(ctx context.Context, sessionKey, prompt string) error

// PromptJobArgs defines a scheduled job that sends a prompt to the agent.
type PromptJobArgs struct {
	Prompt     string `json:"prompt"`
	SessionKey string `json:"session_key"`
}

func (PromptJobArgs) Kind() string { return "prompt" }

// promptWorker handles scheduled prompt jobs.
type promptWorker struct {
	river.WorkerDefaults[PromptJobArgs]
	handler PromptHandler
}

func (w *promptWorker) Work(ctx context.Context, job *river.Job[PromptJobArgs]) error {
	slog.Info("cron: executing job",
		"job_id", job.ID,
		"session_key", job.Args.SessionKey,
		"prompt_len", len(job.Args.Prompt),
	)
	return w.handler(ctx, job.Args.SessionKey, job.Args.Prompt)
}
