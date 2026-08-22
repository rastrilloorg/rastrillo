CREATE TABLE IF NOT EXISTS auth_links (
  hash       TEXT PRIMARY KEY,
  address    TEXT NOT NULL,
  purpose    TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_sessions (
  token_hash TEXT PRIMARY KEY,
  address    TEXT NOT NULL,
  method     TEXT NOT NULL,
  auth_time  TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
