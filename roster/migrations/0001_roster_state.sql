CREATE TABLE IF NOT EXISTS roster_state (
  id          INTEGER PRIMARY KEY CHECK (id = 1),
  sequence    INTEGER NOT NULL,
  state       TEXT NOT NULL,
  members     TEXT NOT NULL DEFAULT '[]',
  lease_until INTEGER NOT NULL DEFAULT 0
);
