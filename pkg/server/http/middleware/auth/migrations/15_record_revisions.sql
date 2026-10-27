-- A new revision is assigned by PostgreSQL on every write, including LDAP,
-- internal updates and direct SQL. Never reuse a revision after delete/recreate.
CREATE SEQUENCE auth_record_revision_seq;

CREATE FUNCTION auth_assign_record_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.revision := 'rev-' || nextval('auth_record_revision_seq')::text;
    IF TG_NARGS > 0 THEN
        NEW := jsonb_populate_record(NEW, jsonb_build_object(
            TG_ARGV[0], coalesce(to_jsonb(NEW)->TG_ARGV[0], '{}'::jsonb)
                || jsonb_build_object('revision', NEW.revision)
        ));
    END IF;
    RETURN NEW;
END $$;

DO $$
DECLARE item record;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('auth_settings', ''),
        ('auth_oauth_clients', ''),
        ('auth_oauth_providers', ''),
        ('auth_ldap_configs', ''),
        ('auth_saml_providers', ''),
        ('auth_users', 'doc'),
        ('auth_roles', 'config'),
        ('auth_permissions', 'config'),
        ('auth_lmaps', 'doc')
    ) AS targets(table_name, document_column)
    LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN revision text NOT NULL DEFAULT ''''', item.table_name);
        IF item.document_column = '' THEN
            EXECUTE format('CREATE TRIGGER auth_record_revision BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION auth_assign_record_revision()', item.table_name);
        ELSE
            EXECUTE format('CREATE TRIGGER auth_record_revision BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION auth_assign_record_revision(%L)', item.table_name, item.document_column);
        END IF;
        EXECUTE format('UPDATE %I SET revision = revision', item.table_name);
    END LOOP;
END $$;
