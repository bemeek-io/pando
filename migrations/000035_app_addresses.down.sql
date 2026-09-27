DROP INDEX IF EXISTS apps_address_path_unique;
DROP INDEX IF EXISTS apps_address_hostname_unique;
ALTER TABLE apps
    DROP COLUMN IF EXISTS address_path,
    DROP COLUMN IF EXISTS address_hostname;
