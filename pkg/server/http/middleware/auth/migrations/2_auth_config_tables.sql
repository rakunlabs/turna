CREATE TABLE IF NOT EXISTS auth_oauth_clients (
    id text PRIMARY KEY,
    config_encrypted text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT 'unknown'
);

CREATE TABLE IF NOT EXISTS auth_oauth_providers (
    id text PRIMARY KEY,
    config_encrypted text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT 'unknown'
);

CREATE TABLE IF NOT EXISTS auth_ldap_configs (
    id text PRIMARY KEY,
    config_encrypted text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT 'unknown'
);

CREATE TABLE IF NOT EXISTS auth_users (
    id text PRIMARY KEY,
    alias text[] NOT NULL DEFAULT '{}',
    details_encrypted text,
    disabled boolean NOT NULL DEFAULT false,
    service_account boolean NOT NULL DEFAULT false,
    local boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT 'unknown'
);

CREATE INDEX IF NOT EXISTS auth_users_alias_gin_idx
ON auth_users USING gin (alias);

CREATE INDEX IF NOT EXISTS auth_users_service_account_idx
ON auth_users (service_account);

CREATE TABLE IF NOT EXISTS auth_roles (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT 'unknown'
);

CREATE TABLE IF NOT EXISTS auth_permissions (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT 'unknown'
);

CREATE TABLE IF NOT EXISTS auth_lmaps (
    name text PRIMARY KEY,
    role_ids text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT 'unknown'
);
