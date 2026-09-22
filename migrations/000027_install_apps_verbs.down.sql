-- Custom roles holding either verb keep it, and it means nothing once the
-- authorizer no longer reads it; remove them from custom roles by hand if a
-- rollback needs to be tidy.
ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;

UPDATE roles
SET verbs = array_remove(array_remove(verbs, 'install.apps.view'::text), 'install.apps.manage'::text)
WHERE id = 'role_administrator';

ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;
