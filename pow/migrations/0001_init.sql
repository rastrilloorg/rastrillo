CREATE TABLE IF NOT EXISTS pow_spent_nonces (
  nonce      TEXT PRIMARY KEY,
  expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS pow_spent_nonces_expires_at
  ON pow_spent_nonces (expires_at);
