-- rastrillo: post-adoption
-- Only for apps whose events carry money (eventlog.MoneySchema). A
-- currency-unaware binary then fails on every read, append and ingest
-- instead of misreading amounts. The snapshot records, per stream and
-- writer, the last event written before the upgrade, so replay can
-- refuse an unversioned event an old binary appended afterwards.
ALTER TABLE eventlog RENAME COLUMN payload TO payload_v2;
CREATE TABLE eventlog_upgrade (
  stream TEXT    NOT NULL,
  writer TEXT    NOT NULL,
  seq    INTEGER NOT NULL,
  PRIMARY KEY (stream, writer)
);
INSERT INTO eventlog_upgrade (stream, writer, seq)
  SELECT stream, writer, MAX(seq) FROM eventlog GROUP BY stream, writer;
