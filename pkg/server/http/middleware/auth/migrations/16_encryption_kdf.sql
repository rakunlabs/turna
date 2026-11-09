ALTER TABLE auth_encryption_check
    ADD COLUMN IF NOT EXISTS kdf_salt bytea;
