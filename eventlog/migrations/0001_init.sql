CREATE TABLE IF NOT EXISTS eventlog (
  stream  TEXT    NOT NULL,
  writer  TEXT    NOT NULL,
  seq     INTEGER NOT NULL,
  lamport INTEGER NOT NULL,
  ts      TEXT    NOT NULL,
  actor   TEXT    NOT NULL,
  kind    TEXT    NOT NULL,
  payload TEXT    NOT NULL,
  PRIMARY KEY (stream, writer, seq)
);

CREATE INDEX IF NOT EXISTS eventlog_merge_order
  ON eventlog (stream, lamport, ts, writer, seq);
