-- Back to the two bundles. A role holding all thirteen of App manager's verbs
-- gets install.apps.manage back; every install.apps.* verb but
-- install.apps.view is then removed, which takes away a custom role's
-- narrower install-wide verbs — the older version has no way to say them.
-- Grants of the three built-ins are deleted with them.
ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;

DELETE FROM grants WHERE role_id IN ('role_app_viewer', 'role_app_manager', 'role_auditor');
DELETE FROM roles WHERE id IN ('role_app_viewer', 'role_app_manager', 'role_auditor');

UPDATE roles
SET verbs = verbs || 'install.apps.manage'::text
WHERE verbs @> ARRAY[
    'install.apps.view',
    'install.apps.logs.read',
    'install.apps.deploy',
    'install.apps.restart',
    'install.apps.spec.edit',
    'install.apps.secrets.write',
    'install.apps.secrets.read',
    'install.apps.exec',
    'install.apps.grants.manage',
    'install.apps.routing.override',
    'install.apps.resources.override',
    'install.apps.egress.tighten',
    'install.apps.egress.loosen',
    'install.apps.delete'
]::text[];

UPDATE roles
SET verbs = ARRAY(
    SELECT v FROM unnest(verbs) WITH ORDINALITY AS t(v, ord)
    WHERE v = 'install.apps.view' OR v = 'install.apps.manage' OR v NOT LIKE 'install.apps.%'
    ORDER BY ord
)
WHERE EXISTS (SELECT 1 FROM unnest(verbs) v WHERE v LIKE 'install.apps.%');

ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;
