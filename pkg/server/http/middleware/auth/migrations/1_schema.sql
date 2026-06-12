CREATE TABLE IF NOT EXISTS auth_versions (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    version bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO auth_versions (id, version)
VALUES (true, 1)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS auth_settings (
    namespace text PRIMARY KEY,
    value_encrypted text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT 'unknown'
);

CREATE TABLE IF NOT EXISTS auth_events (
    version bigint PRIMARY KEY,
    topic text NOT NULL,
    action text NOT NULL,
    entity_id text NOT NULL,
    payload jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_events_topic_version_idx
ON auth_events (topic, version);
