CREATE TABLE IF NOT EXISTS handoff_used (
  jti        TEXT PRIMARY KEY,
  expires_at INTEGER NOT NULL
);
