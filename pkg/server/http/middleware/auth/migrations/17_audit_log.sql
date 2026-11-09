CREATE TABLE IF NOT EXISTS auth_audit_log (
    id bigserial PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    event text NOT NULL,
    outcome text NOT NULL,
    actor text NOT NULL DEFAULT '',
    subject text NOT NULL DEFAULT '',
    client_id text NOT NULL DEFAULT '',
    ip text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT '',
    detail jsonb
);

CREATE INDEX IF NOT EXISTS auth_audit_log_created_idx ON auth_audit_log (created_at DESC);
CREATE INDEX IF NOT EXISTS auth_audit_log_event_idx ON auth_audit_log (event, created_at DESC);
CREATE INDEX IF NOT EXISTS auth_audit_log_subject_idx ON auth_audit_log (subject, created_at DESC);
