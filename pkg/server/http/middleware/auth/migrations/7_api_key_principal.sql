ALTER TABLE auth_api_keys
ADD COLUMN IF NOT EXISTS role_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
ADD COLUMN IF NOT EXISTS permission_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
ADD COLUMN IF NOT EXISTS details jsonb NOT NULL DEFAULT '{}'::jsonb,
ADD COLUMN IF NOT EXISTS disabled boolean NOT NULL DEFAULT false,
ADD COLUMN IF NOT EXISTS revision bigint NOT NULL DEFAULT 1,
ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

UPDATE auth_api_keys SET updated_at = created_at WHERE updated_at IS NULL;

UPDATE auth_api_keys AS k
SET role_ids = COALESCE(u.doc->'role_ids', '[]'::jsonb),
    permission_ids = COALESCE(u.doc->'permission_ids', '[]'::jsonb),
    details = jsonb_build_object('owner_user_id', k.user_id)
FROM auth_users AS u
WHERE k.user_id = u.id
  AND k.role_ids = '[]'::jsonb
  AND k.permission_ids = '[]'::jsonb
  AND k.details = '{}'::jsonb;
