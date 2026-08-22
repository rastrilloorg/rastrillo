CREATE TABLE IF NOT EXISTS passkey_credentials (
  id         TEXT PRIMARY KEY,
  subject    TEXT NOT NULL,
  public_key BLOB NOT NULL,
  sign_count INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS passkey_credentials_subject
  ON passkey_credentials (subject);

CREATE TABLE IF NOT EXISTS passkey_challenges (
  challenge  TEXT PRIMARY KEY,
  subject    TEXT NOT NULL,
  purpose    TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS passkey_pending (
  token_hash TEXT PRIMARY KEY,
  subject    TEXT NOT NULL,
  method     TEXT NOT NULL DEFAULT '',
  return_to  TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS passkey_recovery_codes (
  code_hash  TEXT PRIMARY KEY,
  subject    TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS passkey_recovery_codes_subject
  ON passkey_recovery_codes (subject);
