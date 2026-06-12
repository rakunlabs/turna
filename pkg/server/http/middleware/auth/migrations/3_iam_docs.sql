ALTER TABLE auth_users ADD COLUMN IF NOT EXISTS doc jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE auth_lmaps ADD COLUMN IF NOT EXISTS doc jsonb NOT NULL DEFAULT '{}'::jsonb;
