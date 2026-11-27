-- Runtime-editable instance settings. The config file stays operator-owned; a
-- value here is used only when the equivalent config key is unset.
CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

-- One row per `lewnotes login` attempt. The handoff column holds the encrypted
-- envelope between approve and the first successful poll.
CREATE TABLE cli_login_requests (
  id            TEXT PRIMARY KEY,
  public_key    BLOB NOT NULL,
  state_hash    TEXT NOT NULL,
  device_name   TEXT NOT NULL,
  client        TEXT NOT NULL,
  redirect_uri  TEXT NOT NULL DEFAULT '',
  server_origin TEXT NOT NULL,
  user_code     TEXT NOT NULL,
  status        TEXT NOT NULL,
  user_id       TEXT,
  token_id      TEXT,
  handoff       TEXT,
  attempts      INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL,
  expires_at    INTEGER NOT NULL,
  approved_at   INTEGER,
  consumed_at   INTEGER
);
CREATE UNIQUE INDEX cli_login_requests_code ON cli_login_requests(user_code) WHERE status = 'pending';
CREATE INDEX cli_login_requests_expiry ON cli_login_requests(expires_at);
