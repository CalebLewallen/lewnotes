-- 0002: server-only state. Unlike 0001, this data is NOT rebuildable from the
-- vault: deleting the database signs everyone out and requires setup again.
-- Note: session IDs are stored hashed, so a database leak does not hand over
-- live sessions.

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    instance_name TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at    INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE sessions (
    id_hash      TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token   TEXT NOT NULL,
    created_at   INTEGER NOT NULL DEFAULT (unixepoch()),
    last_seen_at INTEGER NOT NULL DEFAULT (unixepoch()),
    expires_at   INTEGER NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT '',
    ip           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(expires_at);

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL,
    prefix       TEXT NOT NULL,
    scope        TEXT NOT NULL DEFAULT 'full',
    created_at   INTEGER NOT NULL DEFAULT (unixepoch()),
    last_used_at INTEGER,
    revoked_at   INTEGER,
    user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX api_tokens_user ON api_tokens(user_id);
