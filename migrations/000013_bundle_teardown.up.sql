-- Tearing down a deleted app's bundle.
--
-- Nothing has ever called RuntimeAdapter.Destroy. Deleting an app archived the
-- row and left its containers running on a private network that was never
-- reclaimed — and Docker's default address pool holds about thirty, so an
-- install that adds and removes apps eventually cannot start one. The error it
-- fails with names subnets, which tells an operator nothing about what happened.
--
-- Recorded on the app rather than inferred from the runtime, because "is this
-- bundle gone" is a question only the adapter can answer and asking it on every
-- GC pass, for every app ever deleted, is a conversation that grows forever.
ALTER TABLE apps ADD COLUMN bundle_destroyed_at timestamptz;

-- The GC query: archived, bundle still standing.
CREATE INDEX apps_awaiting_teardown_idx ON apps (deleted_at)
    WHERE deleted_at IS NOT NULL AND bundle_destroyed_at IS NULL;

-- Apps deleted before this migration have bundles that may still be running,
-- and they are exactly the ones leaking. Left NULL so the first GC pass after
-- an upgrade collects them, which is the point.
COMMENT ON COLUMN apps.bundle_destroyed_at IS
    'When the runtime confirmed the bundle was gone. NULL means teardown is still
     owed. Volumes are never part of this: they outlive the app by design (R-204)
     and are destroyed only by an explicit act.';
