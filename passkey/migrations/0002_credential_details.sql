-- rastrillo: post-adoption
-- What a credential is, beyond the key that speaks for it: a name the
-- person gave it, the authenticator's make (AAGUID), whether it verified
-- the user at registration, whether it is a synced passkey or bound to
-- one device, and when it was last used. Existing rows were registered
-- under a policy that required user verification, so they keep that.
ALTER TABLE passkey_credentials ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE passkey_credentials ADD COLUMN aaguid TEXT NOT NULL DEFAULT '';
ALTER TABLE passkey_credentials ADD COLUMN user_verified INTEGER NOT NULL DEFAULT 1;
ALTER TABLE passkey_credentials ADD COLUMN backup_eligible INTEGER NOT NULL DEFAULT 0;
ALTER TABLE passkey_credentials ADD COLUMN backup_state INTEGER NOT NULL DEFAULT 0;
ALTER TABLE passkey_credentials ADD COLUMN last_used_at TEXT NOT NULL DEFAULT '';
