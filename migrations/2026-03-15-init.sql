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

CREATE TABLE IF NOT EXISTS bugfixes (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    linear_issue_id      TEXT NOT NULL,                          -- e.g. "PLA-11"
    linear_issue_url     TEXT,
    title                TEXT NOT NULL,                          -- issue title
    status               TEXT NOT NULL DEFAULT 'pending',        -- pending, assessing, running, review, stuck, done, failed, killed
    worktree_path        TEXT,                                   -- e.g. /home/user/src/main/kevin-1
    branch               TEXT,                                   -- e.g. kevin/PLA-11-scandinavian-search
    session_id           TEXT,                                   -- Claude session ID for --resume
    pr_url               TEXT,                                   -- draft PR URL once created
    pr_merged            BOOLEAN NOT NULL DEFAULT false,
    pr_iterations        INT NOT NULL DEFAULT 0,                 -- review rounds (push → feedback → push)
    pr_last_checked_at   TIMESTAMPTZ,                            -- last time we checked PR status
    confidence           JSONB,                                  -- {"clarity":"high","localizability":"medium","testability":"high"}
    log_path             TEXT,                                   -- memory/runs/{id}.md
    last_human_update_at TIMESTAMPTZ,                            -- last time Kevin messaged owner
    time_budget          INTERVAL NOT NULL DEFAULT '1 hour',
    tokens_used          BIGINT NOT NULL DEFAULT 0,              -- estimated total tokens spent
    killed_by            TEXT,                                   -- owner, timeout, loop_detected, token_budget (null if not killed)
    killed_at            TIMESTAMPTZ,
    error                TEXT,                                   -- last error message if failed/stuck
    started_at           TIMESTAMPTZ,
    finished_at          TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_bugfixes_status ON bugfixes (status);
CREATE INDEX IF NOT EXISTS idx_bugfixes_issue ON bugfixes (linear_issue_id);

CREATE TABLE IF NOT EXISTS sessions (
    session_key     TEXT PRIMARY KEY,
    claude_session  TEXT NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
