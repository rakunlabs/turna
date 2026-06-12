CREATE TABLE IF NOT EXISTS auth_passkey_credentials (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    name text NOT NULL DEFAULT '',
    credential jsonb NOT NULL,
    sign_count bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_passkey_credentials_user_idx
ON auth_passkey_credentials (user_id);
