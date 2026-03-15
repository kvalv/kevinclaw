package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kvalv/kevinclaw/internal/agent"
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

// RecentMessages returns recent messages for a channel/thread, ordered oldest first.
// For threads (threadTS non-empty): returns all messages in that thread.
// For channels (threadTS empty): returns the last `limit` top-level messages.
func (d *DB) RecentMessages(ctx context.Context, channel, threadTS string, limit int) ([]agent.Message, error) {
	var query string
	var args []any

	if threadTS != "" {
		query = `SELECT user_id, text, created_at FROM messages
			WHERE channel = $1 AND thread_ts = $2
			ORDER BY created_at ASC`
		args = []any{channel, threadTS}
	} else {
		query = `SELECT user_id, text, created_at FROM messages
			WHERE channel = $1 AND thread_ts IS NULL
			ORDER BY created_at DESC LIMIT $2`
		args = []any{channel, limit}
	}

	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	var msgs []agent.Message
	for rows.Next() {
		var m agent.Message
		var ts time.Time
		if err := rows.Scan(&m.UserID, &m.Text, &ts); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		m.Timestamp = ts.Format(time.RFC3339)
		msgs = append(msgs, m)
	}

	// For channel queries we got DESC order, reverse to oldest-first
	if threadTS == "" {
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
	}

	return msgs, nil
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
