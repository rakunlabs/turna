CREATE TABLE IF NOT EXISTS auth_encryption_check (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    canary_encrypted text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
