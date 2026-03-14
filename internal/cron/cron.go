package cron

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// PromptJobArgs defines a scheduled job that sends a prompt to the agent.
type PromptJobArgs struct {
	Prompt     string `json:"prompt"`
	SessionKey string `json:"session_key"`
}

func (PromptJobArgs) Kind() string { return "prompt" }

// PromptHandler is called when a scheduled prompt job fires.
type PromptHandler func(ctx context.Context, sessionKey, prompt string) error

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

// Stop gracefully shuts down the River client.
func (s *Scheduler) Stop(ctx context.Context) error {
	return s.client.Stop(ctx)
}

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
