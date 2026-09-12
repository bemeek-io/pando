-- Install-level authorization (O-17, R-265).
--
-- Until now every grant was app-scoped — grants.app_id was NOT NULL — and there
-- was no way to express "may manage this installation". So six endpoints were
-- gated by "are you signed in" and nothing else, and a user with no grants at
-- all could suspend the administrator and lock the install out.
--
-- The fix keeps one authorization model. An administrator is someone holding a
-- grant, the same as everyone else; the grant simply has no app.

-- Roles are scoped, because a role carrying install verbs granted on a single
-- app is nonsense and a role carrying app verbs granted install-wide is worse.
ALTER TABLE roles ADD COLUMN scope text NOT NULL DEFAULT 'app'
    CHECK (scope IN ('app', 'install'));

-- Needed for the composite foreign key below.
CREATE UNIQUE INDEX roles_id_scope_key ON roles (id, scope);

-- R-081 gains a fourth built-in role, install-scoped. Immutable like the other
-- three: the trigger on roles already refuses UPDATE and DELETE on a builtin,
-- so new verbs reach it by migration and no other way.
--
-- Verb set from R-080's table. app.create is here rather than in an app role
-- because there is no app yet when it is checked — Sequence A step 1 has always
-- called it install-level.
INSERT INTO roles (id, name, builtin, scope, verbs) VALUES
    ('role_administrator', 'administrator', true, 'install', ARRAY[
        'install.view',
        'install.users.manage',
        'install.policy.manage',
        'install.adapters.manage',
        'install.audit.read',
        'app.create'
    ]);

-- An install-scoped grant has no app.
ALTER TABLE grants ALTER COLUMN app_id DROP NOT NULL;

-- Which scope a grant is, carried on the row so the database can enforce the
-- correspondence rather than trusting the application to.
ALTER TABLE grants ADD COLUMN role_scope text NOT NULL DEFAULT 'app'
    CHECK (role_scope IN ('app', 'install'));

-- The two halves of the guarantee, both structural:
--
--   1. a grant's role_scope matches its role's actual scope, via a composite
--      foreign key — so an install role cannot be granted on an app, and an app
--      role cannot be granted install-wide;
--   2. an install-scoped grant has no app_id and an app-scoped one does.
--
-- Together these mean "app_id IS NULL" and "carries install verbs" cannot come
-- apart, which is the property the NOT NULL was previously providing for free.
ALTER TABLE grants
    ADD CONSTRAINT grants_role_scope_matches_role
    FOREIGN KEY (role_id, role_scope) REFERENCES roles (id, scope);

ALTER TABLE grants
    ADD CONSTRAINT grants_install_has_no_app CHECK (
        (role_scope = 'install' AND app_id IS NULL) OR
        (role_scope = 'app'     AND app_id IS NOT NULL)
    );

-- Data-plane use is per-app and binary (R-070): there is no install-wide "use",
-- so a data grant always names an app. This was implied by app_id NOT NULL and
-- has to be said now that it is nullable.
ALTER TABLE grants
    ADD CONSTRAINT grants_data_plane_is_app_scoped CHECK (
        plane = 'control' OR app_id IS NOT NULL
    );

-- No new uniqueness index is needed. grants_unique_principal is
-- (app_id, plane, principal_kind, principal_id) NULLS NOT DISTINCT, and
-- NULLS NOT DISTINCT means a NULL app_id compares equal to itself — so it
-- already reads "one control grant per principal, install-wide" for these rows,
-- which is the same rule it enforces per app.
--
-- One install role per principal, then. A combination of privileges is a custom
-- role composed from the verb list (R-082), not two grants — which is also why
-- there is no implication graph to get wrong.
