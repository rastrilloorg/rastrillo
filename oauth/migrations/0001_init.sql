CREATE TABLE oauth_clients (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  redirects     TEXT NOT NULL,
  document      INTEGER NOT NULL,
  registered_at TEXT NOT NULL,
  used_at       TEXT NOT NULL
);

CREATE INDEX oauth_clients_used ON oauth_clients (used_at);

CREATE TABLE oauth_connections (
  id          TEXT PRIMARY KEY,
  subject     TEXT NOT NULL,
  client_id   TEXT NOT NULL,
  client_name TEXT NOT NULL,
  return_host TEXT NOT NULL,
  resource    TEXT NOT NULL,
  access      TEXT NOT NULL CHECK (access IN ('read', 'write')),
  created_at  TEXT NOT NULL,
  last_used   TEXT NOT NULL,
  ended_at    TEXT
);

CREATE INDEX oauth_connections_subject ON oauth_connections (subject);

CREATE TABLE oauth_pending (
  id          TEXT PRIMARY KEY,
  subject     TEXT NOT NULL,
  client_id   TEXT NOT NULL,
  client_name TEXT NOT NULL,
  redirect    TEXT NOT NULL,
  challenge   TEXT NOT NULL,
  resource    TEXT NOT NULL,
  -- NULL when the request sent no state; '' when it sent an empty one.
  state       TEXT,
  return_to   TEXT NOT NULL,
  requested   TEXT NOT NULL CHECK (requested IN ('read', 'write')),
  expires_at  TEXT NOT NULL
);

CREATE INDEX oauth_pending_expires ON oauth_pending (expires_at);

CREATE TABLE oauth_codes (
  hash       BLOB PRIMARY KEY,
  connection TEXT NOT NULL,
  client_id  TEXT NOT NULL,
  redirect   TEXT NOT NULL,
  challenge  TEXT NOT NULL,
  resource   TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at    TEXT
);

CREATE INDEX oauth_codes_expires ON oauth_codes (expires_at);

CREATE TABLE oauth_refresh (
  hash       BLOB PRIMARY KEY,
  connection TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at    TEXT
);

CREATE INDEX oauth_refresh_expires ON oauth_refresh (expires_at);

CREATE INDEX oauth_refresh_connection ON oauth_refresh (connection);
