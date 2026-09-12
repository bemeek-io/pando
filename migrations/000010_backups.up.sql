-- Backups and disaster recovery (design 02 §2.8, Sequence D).

-- R-080 gains a seventh install-scoped verb. Taking and restoring backups is
-- not the same power as rewriting host policy — a restore replaces the whole
-- install, and folding it into an existing verb would hand that to anyone who
-- could edit a source allowlist.
--
-- R-081 says built-in role contents change by migration and no other way, and
-- the trigger enforces it at runtime. This is that sanctioned path: disable,
-- amend, re-enable.
ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;

-- The ::text cast is load-bearing. Without it Postgres reads `text[] || unknown`
-- as array-concatenates-array and rejects the literal as a malformed array.
UPDATE roles
SET verbs = verbs || 'install.backup.manage'::text
WHERE id = 'role_administrator';

ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;

-- R-252 gained an eighth category, so the column that enumerates them has to
-- agree. The CHECK is the reason this is a migration rather than a Go constant:
-- the database is where "these are the categories" is actually enforced, and a
-- new category that only exists in Go is a category no adapter can be
-- configured in.
ALTER TABLE adapter_configs DROP CONSTRAINT adapter_configs_category_check;
ALTER TABLE adapter_configs ADD CONSTRAINT adapter_configs_category_check
    CHECK (category IN ('runtime', 'routing', 'builder', 'secrets',
                        'services', 'identity', 'notify', 'backup'));

CREATE TABLE backups (
    id           text PRIMARY KEY,            -- bkp_...

    -- NULL for full-host DR bundles. A per-app backup names its app; losing
    -- the app must not lose the record of its final backup, which is why this
    -- does not cascade — see the FK below.
    app_id       text REFERENCES apps(id) ON DELETE SET NULL,

    kind         text NOT NULL CHECK (kind IN ('rolling', 'on_delete', 'dr_bundle')),

    -- Which destination adapter holds it, and under what name. The adapter ref
    -- is stored rather than resolved at restore time: a bundle written to one
    -- destination is not findable in another, and silently looking elsewhere
    -- is how a restore reports "not found" for a bundle that exists.
    adapter_ref  text NOT NULL,
    object_name  text NOT NULL,

    size_bytes   bigint,

    -- What is inside. Drives restore verification (R-215) — this is the copy
    -- Pando trusts, and the copy inside the bundle is the one being checked.
    manifest     jsonb NOT NULL,

    -- NULL means kept until explicitly discarded. Required for on_delete
    -- (R-204) and used by dr_bundle when the destination owns retention, in
    -- which case Pando records and does not prune.
    retain_until timestamptz,

    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),

    -- R-204: a final backup taken because an app was deleted is kept until
    -- someone discards it. Ageing one out would defeat the reason it was
    -- taken, so the schema refuses to let it carry an expiry at all.
    CONSTRAINT backups_on_delete_is_never_aged_out CHECK (
        kind <> 'on_delete' OR retain_until IS NULL
    ),

    -- A DR bundle is the whole install, so it names no app; the per-app kinds
    -- must name one, or "restore this backup" has no target (R-206).
    CONSTRAINT backups_scope_matches_kind CHECK (
        (kind = 'dr_bundle' AND app_id IS NULL) OR
        (kind <> 'dr_bundle' AND app_id IS NOT NULL)
    ),

    -- One row per stored object. Writing the same object twice would leave two
    -- rows disagreeing about what is in one file.
    CONSTRAINT backups_object_is_unique UNIQUE (adapter_ref, object_name)
);

CREATE INDEX backups_app_idx ON backups (app_id, created_at DESC);

-- The prune query: rolling backups whose retention has passed (R-211).
CREATE INDEX backups_retention_idx ON backups (retain_until)
    WHERE retain_until IS NOT NULL;

COMMENT ON COLUMN backups.app_id IS
    'NULL for dr_bundle. ON DELETE SET NULL rather than CASCADE: R-204 keeps the
     final backup after the app is gone, and cascading would delete the record of
     the backup at exactly the moment it starts mattering.';
