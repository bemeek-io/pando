-- A volume's ID is unique within its app, not across the installation
-- (issue #87, R-204, R-211).
--
-- A volume row's ID is the spec's volume ID (state.Volumes.RecordFromRuntime),
-- and a spec names its volumes after what they hold: "data", "pgdata",
-- "uploads". Two apps that both keep a volume called "data" are ordinary. With
-- the ID alone as the key, the second app's deploy hit the first app's row,
-- rewrote its handle to the second app's volume, and recorded nothing for the
-- second app. From then on:
--
--   - the second app had no volume rows, so its rolling backups found nothing
--     to copy and quietly did nothing, every hour (R-211);
--   - the first app's rolling backups copied the second app's data;
--   - the second app lost R-203's protection: a volume Pando has no row for is
--     one it believes never held data, and recreates empty.
--
-- The key is the pair. Existing rows are already unique by ID, so this cannot
-- fail on them; the rows the collision lost are recorded again by the
-- reconciler from what the runtime reports (Reconciler.recordObservedVolumes).
ALTER TABLE volumes DROP CONSTRAINT volumes_pkey;
ALTER TABLE volumes ADD PRIMARY KEY (app_id, id);

-- The last scheduled backup attempt for each app, whatever came of it.
--
-- A rolling backup that did not happen used to leave nothing behind but a
-- line in the server log, and sometimes not even that. An app that has not
-- been backed up for a week looked, on every screen, exactly like one that
-- had. One row per app, replaced on every attempt: this answers "what
-- happened last time and what do I do about it", and the backups table
-- already answers "what do I have".
CREATE TABLE backup_attempts (
    app_id       text PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    attempted_at timestamptz NOT NULL,
    outcome      text NOT NULL CHECK (outcome IN ('taken', 'skipped', 'failed')),

    -- Set when outcome is 'taken'. Not a foreign key: expiry removes old
    -- backups, and the record of an attempt outlives the object it made.
    backup_id    text,

    -- Why it was skipped or failed, and what to do about it (R-105). Empty
    -- when taken.
    message      text NOT NULL DEFAULT '',
    remedy       text NOT NULL DEFAULT ''
);
