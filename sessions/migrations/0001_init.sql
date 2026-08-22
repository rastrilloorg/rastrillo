CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  subject    TEXT NOT NULL,
  method     TEXT NOT NULL DEFAULT '',
  auth_time  TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
