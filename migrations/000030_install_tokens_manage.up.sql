-- Service tokens get their own verb (R-080, R-060).
--
-- install.tokens.manage: list, create and revoke service tokens. It used to be
-- install.users.manage's, and issuing a credential for an automation is a
-- different trust from managing people. The Administrator gets it here, by the
-- sanctioned path for changing a built-in role (R-081); a custom role that held
-- install.users.manage for the sake of tokens needs it added.
ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;

UPDATE roles
SET verbs = verbs || 'install.tokens.manage'::text
WHERE id = 'role_administrator';

ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;
