CREATE TABLE IF NOT EXISTS auth_sync_locks (
    name text PRIMARY KEY,
    instance_id text NOT NULL,
    acquired_at timestamptz NOT NULL DEFAULT now()
);
