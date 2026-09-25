ALTER TABLE auth_passkey_credentials
ADD COLUMN IF NOT EXISTS rp_id text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS auth_passkey_credentials_user_rp_idx
ON auth_passkey_credentials (user_id, rp_id);
