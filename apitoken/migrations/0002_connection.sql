ALTER TABLE api_tokens ADD COLUMN connection TEXT NOT NULL DEFAULT '';

CREATE INDEX api_tokens_connection ON api_tokens (connection);
