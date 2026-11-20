-- The custom origin of the last applied snapshot, read at boot before
-- the app builds its handlers. Empty means the instance's own host.
ALTER TABLE roster_state ADD COLUMN origin TEXT NOT NULL DEFAULT '';
