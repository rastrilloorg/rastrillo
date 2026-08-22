CREATE TABLE IF NOT EXISTS blobs (
  hash         TEXT    PRIMARY KEY,
  content_type TEXT    NOT NULL,
  size         INTEGER NOT NULL,
  data         BLOB    NOT NULL
);
