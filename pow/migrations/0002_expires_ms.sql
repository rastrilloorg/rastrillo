-- v2 seals an absolute expiry in milliseconds and the spend compares it
-- with SQLite's own clock inside the INSERT, so the column must be a
-- number SQL can compare; the RFC 3339 text it replaces cannot be.
-- No app had imported pow when this was written, so the table is
-- recreated rather than converted row by row.
DROP TABLE IF EXISTS pow_spent_nonces;

CREATE TABLE pow_spent_nonces (
  nonce      TEXT PRIMARY KEY,
  expires_ms INTEGER NOT NULL
);

CREATE INDEX pow_spent_nonces_expires_ms ON pow_spent_nonces (expires_ms);
