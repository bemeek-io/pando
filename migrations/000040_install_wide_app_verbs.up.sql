-- An install-wide counterpart for every app verb (issue #81). R-080, R-081.
--
-- install.apps.deploy is app.deploy on every app, install.apps.secrets.read is
-- app.secrets.read on every app, and so on, one install verb per app verb; the
-- authorizer reads them in one place, CheckControl, through the table in
-- authz.everyApp. A group gets "read every app's logs" once, rather than a
-- grant on each app that somebody must remember to repeat for the next.
--
-- The two bundles that came before become built-in roles made of the new
-- verbs: install.apps.view (the Viewer's verbs on every app) is App viewer, and
-- install.apps.manage (the Owner's on every app) is App manager. install.apps.view
-- stays a verb with a narrower meaning — app.view alone — and every role that
-- held it gains install.apps.logs.read, so nobody loses anything.
-- install.apps.manage leaves the catalog, and every role that held it holds
-- its thirteen verbs instead. app.deploy.approve's counterpart is the existing
-- install.deploys.approve, which no bundle carried and no rewrite here adds.
--
-- Install roles only: these verbs are install-scoped, and the composite key
-- behind R-080 keeps any role holding them out of an app grant, unchanged.

ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;

UPDATE roles
SET verbs = verbs || 'install.apps.logs.read'::text
WHERE 'install.apps.view' = ANY (verbs)
  AND NOT 'install.apps.logs.read' = ANY (verbs);

-- Each verb once, in the order first held.
UPDATE roles r
SET verbs = (
    SELECT array_agg(v ORDER BY first)
    FROM (
        SELECT v, min(ord) AS first
        FROM unnest(array_remove(r.verbs, 'install.apps.manage'::text) || ARRAY[
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
        ]::text[]) WITH ORDINALITY AS t(v, ord)
        GROUP BY v
    ) held
)
WHERE 'install.apps.manage' = ANY (r.verbs);

ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;

-- Three new built-ins take names a custom role may already have (R-082 keeps
-- names unique ignoring case and spaces). As 000028 did: the custom role gets
-- " (custom)", and its ID too if that is taken. Built-in names never change.
UPDATE roles r
SET name = btrim(r.name) || ' (custom)'
WHERE NOT r.builtin
  AND lower(btrim(r.name)) IN ('app viewer', 'app manager', 'auditor')
  AND NOT EXISTS (
    SELECT 1 FROM roles o WHERE lower(btrim(o.name)) = lower(btrim(r.name)) || ' (custom)'
  );

UPDATE roles r
SET name = btrim(r.name) || ' ' || r.id
WHERE NOT r.builtin
  AND lower(btrim(r.name)) IN ('app viewer', 'app manager', 'auditor');

-- An insert, not an update: a new row is how R-081 says the set of built-ins
-- grows. App manager leaves out install.deploys.approve, as Owner leaves out
-- app.deploy.approve (R-155). Auditor sees every app, reads its logs and the
-- audit log, and nothing else — not install.view, so not the accounts.
INSERT INTO roles (id, name, builtin, scope, verbs) VALUES
    ('role_app_viewer', 'app viewer', true, 'install', ARRAY[
        'install.apps.view',
        'install.apps.logs.read'
    ]),
    ('role_app_manager', 'app manager', true, 'install', ARRAY[
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
    ]),
    ('role_auditor', 'auditor', true, 'install', ARRAY[
        'install.audit.read',
        'install.apps.view',
        'install.apps.logs.read'
    ]);

-- install.apps.manage is no longer a verb, and a policy list naming one is
-- refused on its next save. Naming it never denied anything — policy is asked
-- about the app verb, and nothing checked install.apps.manage itself — so
-- dropping it changes no decision.
UPDATE host_policy
SET body = jsonb_set(body, '{disabled_verbs}', (body->'disabled_verbs') - 'install.apps.manage')
WHERE body->'disabled_verbs' ? 'install.apps.manage';

UPDATE host_policy
SET body = jsonb_set(body, '{agent_disabled_verbs}', (body->'agent_disabled_verbs') - 'install.apps.manage')
WHERE body->'agent_disabled_verbs' ? 'install.apps.manage';
