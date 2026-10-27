ALTER TABLE auth_totp_secrets
    ADD COLUMN IF NOT EXISTS last_counter bigint NOT NULL DEFAULT -1,
    ADD COLUMN IF NOT EXISTS failed_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS locked_until timestamptz;
