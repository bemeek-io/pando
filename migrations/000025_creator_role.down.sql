-- The builtin trigger refuses DELETE on a builtin row, so step around it the
-- way 000010 does to change one.
ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;
DELETE FROM grants WHERE role_id = 'role_creator';
DELETE FROM roles WHERE id = 'role_creator';
ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;
