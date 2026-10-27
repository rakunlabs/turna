-- Email aliases are case-insensitive; usernames retain their existing case
-- semantics. Never auto-merge two existing accounts during migration.
CREATE OR REPLACE FUNCTION auth_normalize_alias(value text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE WHEN position('@' IN value) > 0
        THEN lower(btrim(value, E' \t\n\r\f\013'))
        ELSE btrim(value, E' \t\n\r\f\013') END
$$;

DO $$
DECLARE conflict_alias text;
BEGIN
    SELECT auth_normalize_alias(a) INTO conflict_alias
    FROM auth_users u CROSS JOIN LATERAL unnest(u.alias) a
    WHERE auth_normalize_alias(a) <> ''
    GROUP BY auth_normalize_alias(a) HAVING count(DISTINCT u.id) > 1 LIMIT 1;
    IF FOUND THEN
        RAISE EXCEPTION 'Duplicate normalized auth alias: %. Resolve account ownership before migrating.', conflict_alias;
    END IF;
    IF EXISTS (SELECT 1 FROM auth_users u, unnest(u.alias) a WHERE auth_normalize_alias(a) LIKE 'api-key:%') THEN
        RAISE EXCEPTION 'Auth alias uses reserved api-key: prefix. Rename it before migrating.';
    END IF;
END $$;

CREATE TABLE auth_user_aliases (
    alias text PRIMARY KEY CHECK (alias <> '' AND alias NOT LIKE 'api-key:%'),
    user_id text NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE
);
CREATE INDEX auth_user_aliases_user_idx ON auth_user_aliases(user_id);

CREATE OR REPLACE FUNCTION auth_sync_user_aliases() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM auth_user_aliases WHERE user_id = NEW.id;
    INSERT INTO auth_user_aliases(alias, user_id) SELECT a, NEW.id FROM unnest(NEW.alias) a;
    RETURN NEW;
END $$;

-- AFTER INSERT is needed for the user_id FK. Normalization of the user row
-- itself is performed separately before the write.
CREATE OR REPLACE FUNCTION auth_normalize_user_aliases() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.alias := ARRAY(
        SELECT auth_normalize_alias(a) FROM unnest(NEW.alias) WITH ORDINALITY AS t(a, n)
        WHERE auth_normalize_alias(a) <> '' GROUP BY auth_normalize_alias(a) ORDER BY min(n)
    );
    NEW.doc := jsonb_set(NEW.doc, '{alias}', to_jsonb(NEW.alias));
    RETURN NEW;
END $$;

CREATE TRIGGER auth_users_normalize_aliases BEFORE INSERT OR UPDATE OF alias ON auth_users
FOR EACH ROW EXECUTE FUNCTION auth_normalize_user_aliases();
CREATE TRIGGER auth_users_sync_aliases AFTER INSERT OR UPDATE OF alias ON auth_users
FOR EACH ROW EXECUTE FUNCTION auth_sync_user_aliases();

UPDATE auth_users SET alias = alias;
