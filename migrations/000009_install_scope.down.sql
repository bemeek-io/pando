ALTER TABLE grants DROP CONSTRAINT IF EXISTS grants_data_plane_is_app_scoped;
ALTER TABLE grants DROP CONSTRAINT IF EXISTS grants_install_has_no_app;
ALTER TABLE grants DROP CONSTRAINT IF EXISTS grants_role_scope_matches_role;
DELETE FROM grants WHERE app_id IS NULL;
ALTER TABLE grants DROP COLUMN IF EXISTS role_scope;
ALTER TABLE grants ALTER COLUMN app_id SET NOT NULL;

ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;
DELETE FROM roles WHERE id = 'role_administrator';
ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;

DROP INDEX IF EXISTS roles_id_scope_key;
ALTER TABLE roles DROP COLUMN IF EXISTS scope;
