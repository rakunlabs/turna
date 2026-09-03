ALTER TABLE auth_api_keys
ADD COLUMN IF NOT EXISTS description text NOT NULL DEFAULT '';
