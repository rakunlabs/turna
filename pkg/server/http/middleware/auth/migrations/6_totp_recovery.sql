ALTER TABLE auth_totp_secrets
ADD COLUMN IF NOT EXISTS recovery_codes jsonb NOT NULL DEFAULT '[]'::jsonb;
