CREATE TABLE IF NOT EXISTS auth_federated_identities (
    provider text NOT NULL,
    subject text NOT NULL,
    user_id text NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    alias text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);

CREATE INDEX IF NOT EXISTS auth_federated_identities_user_idx
ON auth_federated_identities (user_id);
