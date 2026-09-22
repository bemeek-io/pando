-- Administrators look after every app (R-080, R-081).
--
-- Two install-scoped verbs that bear on apps: install.apps.view, every app
-- read-only, and install.apps.manage, every app verb on every app. The
-- authorizer reads them in one place (CheckControl); what each stands for is
-- the table in authz.everyApp. Neither touches the data plane: using an app
-- still needs a data grant or ownership (R-072, R-087).
--
-- The Administrator gets both, by the sanctioned path for changing a built-in
-- role: disable the trigger, amend, re-enable. A custom role can hold view
-- without manage — someone who can look at every app and change none.
ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;

UPDATE roles
SET verbs = verbs || ARRAY['install.apps.view', 'install.apps.manage']::text[]
WHERE id = 'role_administrator';

ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;
