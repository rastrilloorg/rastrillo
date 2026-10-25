CREATE TABLE api_tokens (
  id          TEXT PRIMARY KEY,
  subject     TEXT NOT NULL,
  agent       TEXT NOT NULL DEFAULT '',
  access      TEXT NOT NULL,
  secret_hash BLOB NOT NULL,
  created_at  TEXT NOT NULL,
  last_used   TEXT,
  expires_at  TEXT,
  revoked_at  TEXT
);

CREATE INDEX api_tokens_subject ON api_tokens (subject);
