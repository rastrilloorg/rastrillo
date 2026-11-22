-- One row per spent call ID. The sweep deletes by expires_at.
CREATE TABLE IF NOT EXISTS rootcall_used (
  jti        TEXT PRIMARY KEY,
  expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS rootcall_used_expires_at
  ON rootcall_used (expires_at);
