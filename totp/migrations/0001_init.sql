CREATE TABLE IF NOT EXISTS totp_secrets (
  subject      TEXT PRIMARY KEY,
  secret       BLOB NOT NULL,
  confirmed_at TEXT NOT NULL DEFAULT '',
  last_counter INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL
);
