ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;

UPDATE roles
SET verbs = array_remove(verbs, 'install.tokens.manage'::text)
WHERE id = 'role_administrator';

ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;
