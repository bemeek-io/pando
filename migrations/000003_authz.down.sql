DROP TABLE IF EXISTS grants;
DROP TRIGGER IF EXISTS roles_builtin_immutable ON roles;
DROP FUNCTION IF EXISTS reject_builtin_role_change();
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS apps;
