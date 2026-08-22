-- Move any pre-sessions-core rows into the sessions core, then empty
-- the old table. Under the ledger this runs exactly once, where the
-- old Migrations []string re-ran it on every boot and relied on the
-- DELETE leaving nothing to copy.
INSERT OR IGNORE INTO sessions (token_hash, subject, method, auth_time, created_at, expires_at)
  SELECT token_hash, address, method, auth_time, created_at, expires_at FROM auth_sessions;

DELETE FROM auth_sessions;
