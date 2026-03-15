CREATE TABLE IF NOT EXISTS messages (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    channel     TEXT NOT NULL,
    thread_ts   TEXT,
    message_ts  TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    user_name   TEXT NOT NULL DEFAULT '',
    text        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_messages_channel_ts ON messages (channel, message_ts);
CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages (channel, thread_ts) WHERE thread_ts IS NOT NULL;

CREATE TABLE IF NOT EXISTS sessions (
    session_key     TEXT PRIMARY KEY,
    claude_session  TEXT NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
