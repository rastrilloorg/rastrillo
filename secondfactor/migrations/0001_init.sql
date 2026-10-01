CREATE TABLE IF NOT EXISTS secondfactor_pending (
  token_hash TEXT PRIMARY KEY,
  subject    TEXT NOT NULL,
  method     TEXT NOT NULL DEFAULT '',
  return_to  TEXT NOT NULL DEFAULT '',
  attempts   INTEGER NOT NULL DEFAULT 0,
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS secondfactor_recovery_codes (
  code_hash  TEXT PRIMARY KEY,
  subject    TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS secondfactor_recovery_codes_subject
  ON secondfactor_recovery_codes (subject);
