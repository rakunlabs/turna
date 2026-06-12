CREATE TABLE IF NOT EXISTS auth_api_keys (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    name text NOT NULL DEFAULT '',
    key_hash text NOT NULL UNIQUE,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);

CREATE INDEX IF NOT EXISTS auth_api_keys_user_idx
ON auth_api_keys (user_id);

CREATE TABLE IF NOT EXISTS auth_totp_secrets (
    user_id text PRIMARY KEY,
    secret_encrypted text NOT NULL,
    confirmed boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- short-lived flow state shared between instances:
-- device codes, email login codes and saml relay states.
CREATE TABLE IF NOT EXISTS auth_flow_codes (
    id text PRIMARY KEY,
    kind text NOT NULL,
    payload jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_flow_codes_expires_idx
ON auth_flow_codes (expires_at);

CREATE TABLE IF NOT EXISTS auth_saml_providers (
    id text PRIMARY KEY,
    config_encrypted text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT 'unknown'
);
