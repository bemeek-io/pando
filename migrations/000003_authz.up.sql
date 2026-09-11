-- Roles, grants, and the minimum of apps that grants must reference.
--
-- Scope note: apps properly belongs to phase 2, but grants cannot exist without
-- something to reference. This creates only the columns grants needs; phase 2
-- adds spec_revisions and the pinned_spec_id FK, which is circular and has to be
-- added after both tables exist.

CREATE TABLE apps (
    id            text PRIMARY KEY,
    name          text NOT NULL,
    slug          text NOT NULL UNIQUE,
    owner_user_id text REFERENCES users(id),  -- R-031

    state         text NOT NULL DEFAULT 'draft',
    desired_state text NOT NULL DEFAULT 'stopped'
                  CHECK (desired_state IN ('running', 'stopped')),

    -- Adapter unreachable. A third field beside state and desired_state because
    -- it answers a different question: whether Pando currently knows either.
    -- Not a state value — an app whose adapter is unreachable has not changed.
    unobservable_since      timestamptz,

    -- Stale-environment detection for R-193. Observe returns no environment, so
    -- a rotated secret is invisible to observation; this is compared state-side.
    -- Hashes (key, version) pairs and literal values, never secret values.
    applied_env_fingerprint text,

    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);

CREATE TABLE roles (
    id         text PRIMARY KEY,
    name       text NOT NULL UNIQUE,
    builtin    boolean NOT NULL DEFAULT false,   -- R-081
    verbs      text[] NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- R-081: built-in roles are immutable. New verbs are added to them by migration
-- in a later Pando version, which is the only sanctioned way their contents
-- change — so the trigger permits nothing at runtime and the migration path
-- disables it explicitly.
CREATE FUNCTION reject_builtin_role_change() RETURNS trigger AS $$
BEGIN
    IF (TG_OP = 'DELETE') THEN
        IF OLD.builtin THEN
            RAISE EXCEPTION 'role % is built in and cannot be deleted (R-081)', OLD.name
                USING ERRCODE = 'insufficient_privilege';
        END IF;
        RETURN OLD;
    END IF;

    IF OLD.builtin THEN
        RAISE EXCEPTION 'role % is built in and cannot be modified (R-081)', OLD.name
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER roles_builtin_immutable
    BEFORE UPDATE OR DELETE ON roles
    FOR EACH ROW EXECUTE FUNCTION reject_builtin_role_change();

-- The three built-in roles (R-081). Verb sets are R-080's table.
--
-- The three *.override verbs each permit deviating from a default the host
-- operator chose, so none of them is in Operator and all three sit with Owner.
INSERT INTO roles (id, name, builtin, verbs) VALUES
    ('role_viewer', 'viewer', true, ARRAY[
        'app.view',
        'app.logs.read'
    ]),
    ('role_operator', 'operator', true, ARRAY[
        'app.view',
        'app.logs.read',
        'app.deploy',
        'app.restart',
        'app.spec.edit',
        'app.secrets.write'
    ]),
    ('role_owner', 'owner', true, ARRAY[
        'app.view',
        'app.logs.read',
        'app.deploy',
        'app.restart',
        'app.spec.edit',
        'app.secrets.write',
        'app.secrets.read',
        'app.exec',
        'app.grants.manage',
        'app.routing.override',
        'app.resources.override',
        'app.egress.override',
        'app.delete'
    ]);

CREATE TABLE grants (
    id             text PRIMARY KEY,
    app_id         text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,

    -- Two planes, never conflated (R-070, R-071). App creation writes two rows,
    -- one per plane, independently revocable (R-073).
    plane          text NOT NULL CHECK (plane IN ('control', 'data')),

    principal_kind text NOT NULL CHECK (principal_kind IN ('user', 'group', 'token', 'anonymous')),

    -- NULL only when kind = anonymous (R-074). The anonymous grant is a real
    -- row, not a flag on the app (R-075).
    principal_id   text,

    -- Control plane only: data-plane use is binary and has no role (R-070).
    role_id        text REFERENCES roles(id),

    created_by     text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT grants_anonymous_has_no_principal CHECK (
        (principal_kind = 'anonymous' AND principal_id IS NULL) OR
        (principal_kind <> 'anonymous' AND principal_id IS NOT NULL)
    ),
    CONSTRAINT grants_role_is_control_plane_only CHECK (
        (plane = 'control' AND role_id IS NOT NULL) OR
        (plane = 'data'    AND role_id IS NULL)
    )
);

-- NULLS NOT DISTINCT so the anonymous grant, whose principal_id is NULL, cannot
-- be inserted twice. Default NULL handling would let duplicates through.
CREATE UNIQUE INDEX grants_unique_principal
    ON grants (app_id, plane, principal_kind, principal_id) NULLS NOT DISTINCT;

CREATE INDEX grants_app_idx ON grants (app_id);
CREATE INDEX grants_principal_idx ON grants (principal_id, plane);
