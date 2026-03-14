package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *DB {
	return &DB{pool: pool}
}

// SaveMessage stores a Slack message.
func (d *DB) SaveMessage(ctx context.Context, channel, threadTS, messageTS, userID, text string) error {
	_, err := d.pool.Exec(ctx,
		`INSERT INTO messages (channel, thread_ts, message_ts, user_id, text) VALUES ($1, NULLIF($2, ''), $3, $4, $5)`,
		channel, threadTS, messageTS, userID, text,
	)
	if err != nil {
		return fmt.Errorf("saving message: %w", err)
	}
	return nil
}

// GetSession returns the claude session ID for a given key, or empty string if not found.
func (d *DB) GetSession(ctx context.Context, sessionKey string) (string, error) {
	var id string
	err := d.pool.QueryRow(ctx,
		`SELECT claude_session FROM sessions WHERE session_key = $1`,
		sessionKey,
	).Scan(&id)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", fmt.Errorf("getting session: %w", err)
	}
	return id, nil
}

// SaveSession upserts the claude session ID for a given key.
func (d *DB) SaveSession(ctx context.Context, sessionKey, claudeSession string) error {
	_, err := d.pool.Exec(ctx,
		`INSERT INTO sessions (session_key, claude_session, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (session_key) DO UPDATE SET claude_session = $2, updated_at = now()`,
		sessionKey, claudeSession,
	)
	if err != nil {
		return fmt.Errorf("saving session: %w", err)
	}
	return nil
}
