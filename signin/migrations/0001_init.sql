CREATE TABLE IF NOT EXISTS signin_passphrases (
  subject    TEXT PRIMARY KEY,
  hash       TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS signin_offers (
  subject      TEXT PRIMARY KEY,
  dismissed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS signin_failures (
  bucket  TEXT NOT NULL,
  at      TEXT NOT NULL,
  attempt TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS signin_failures_bucket_at
  ON signin_failures (bucket, at);

CREATE INDEX IF NOT EXISTS signin_failures_attempt
  ON signin_failures (attempt);
