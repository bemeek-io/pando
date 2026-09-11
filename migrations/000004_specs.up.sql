-- Spec revisions and deployments (design 02 §2.3).

CREATE TABLE spec_revisions (
    id         text PRIMARY KEY,
    app_id     text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    revision   integer NOT NULL,
    origin     text NOT NULL
               CHECK (origin IN ('detected', 'edited', 'redetected', 'imported', 'manual')),
    body       jsonb NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (app_id, revision)
);

CREATE INDEX spec_revisions_app_idx ON spec_revisions (app_id, revision DESC);

-- R-152: append-only. Rollback is repointing at a revision that provably
-- existed, which is only true if a revision can never be edited after the fact.
--
-- Deletion is permitted for the app's own cascade and for retention pruning,
-- but an UPDATE has no legitimate caller at all: editing a spec produces a new
-- revision by definition.
CREATE FUNCTION reject_spec_revision_update() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'spec revisions are append-only; editing a spec creates a new revision (R-152)'
        USING ERRCODE = 'insufficient_privilege';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER spec_revisions_append_only
    BEFORE UPDATE ON spec_revisions
    FOR EACH ROW EXECUTE FUNCTION reject_spec_revision_update();

-- A pinned revision is the one the app is running, or would run. Added here
-- rather than in 000003 because the reference is circular: apps points at
-- spec_revisions, which points back at apps.
ALTER TABLE apps ADD COLUMN pinned_spec_id text REFERENCES spec_revisions(id);

-- R-152 from the other side: a revision that was ever pinned must not be pruned
-- by retention, and that fact has to survive a rollback moving the pointer on.
--
-- Recorded as its own append-only table rather than a flag on spec_revisions.
-- The first attempt was a mutable ever_pinned column, and the append-only
-- trigger above rejected the write — correctly. A table that is append-only
-- except for one column is not append-only, and the exception is what a later
-- change would widen. Pinning is an event, so it is stored as one, which also
-- gives a rollback history for free.
CREATE TABLE spec_pins (
    id         bigserial PRIMARY KEY,
    app_id     text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    spec_id    text NOT NULL REFERENCES spec_revisions(id),
    pinned_by  text NOT NULL,
    pinned_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX spec_pins_spec_idx ON spec_pins (spec_id);
CREATE INDEX spec_pins_app_idx ON spec_pins (app_id, pinned_at DESC);

CREATE TABLE deployments (
    id           text PRIMARY KEY,
    app_id       text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    spec_id      text NOT NULL REFERENCES spec_revisions(id),
    trigger      text NOT NULL
                 CHECK (trigger IN ('manual', 'branch_updated', 'release_tagged', 'rollback')),
    status       text NOT NULL
                 CHECK (status IN ('pending', 'building', 'applying', 'succeeded', 'failed', 'superseded')),
    error_code   text,
    error_detail jsonb,
    started_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    created_by   text NOT NULL
);

CREATE INDEX deployments_app_idx ON deployments (app_id, started_at DESC);

-- Volumes outlive the apps that mount them.
--
-- ON DELETE RESTRICT deliberately (R-204): an app cannot be deleted out from
-- under its volumes, so the delete flow has to resolve them explicitly through
-- the keep-or-discard prompt rather than cascading silently.
CREATE TABLE volumes (
    id          text PRIMARY KEY,
    app_id      text NOT NULL REFERENCES apps(id) ON DELETE RESTRICT,
    name        text NOT NULL,
    adapter_ref text NOT NULL,
    handle      text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (app_id, name)
);

-- No plaintext column exists anywhere (R-190, R-191). The local adapter stores
-- ciphertext; an external adapter stores only a reference.
CREATE TABLE secrets (
    id           text PRIMARY KEY,
    app_id       text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    key          text NOT NULL,
    adapter_ref  text NOT NULL,
    ciphertext   bytea,
    external_ref text,
    version      integer NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (app_id, key)
);
