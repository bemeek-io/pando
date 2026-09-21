-- Security scanning (R-310 – R-320, design 09).
--
-- A scan is a fact about one spec revision and the image built from it: what a
-- scanner found, when, and what that came to as a score. It is not a property
-- of the app, which is why it is a table and not three columns — a rollback
-- restores the score of the revision it rolled back to, with no rescan and no
-- lag, because the row for that revision is still here.
CREATE TABLE app_scans (
    id          text PRIMARY KEY,
    app_id      text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,

    -- The revision scanned. Nullable for a scan of an app that has no pinned
    -- revision yet — somebody scanning a draft before deciding to deploy it.
    spec_id     text REFERENCES spec_revisions(id) ON DELETE SET NULL,

    scanner_ref text NOT NULL,

    -- What the scanner called itself, with its version. "Scanned" is not a fact
    -- on its own: by what, and how long ago, is the rest of it.
    scanner     text NOT NULL DEFAULT '',

    -- 0–100, or NULL when the scan failed. NULL is not zero: unscanned,
    -- unscannable and "scanned and found nothing" are three different states
    -- and a score of 0 is the last of them (R-318).
    score       int CHECK (score IS NULL OR (score BETWEEN 0 AND 100)),

    findings    jsonb NOT NULL DEFAULT '[]'::jsonb,

    -- Why it produced no score, when it produced none. Shown to the app's
    -- owner: a scanner that cannot run is a thing somebody has to fix, and
    -- silence would make it look like the app was clean.
    error       text NOT NULL DEFAULT '',

    ran_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX app_scans_app_idx ON app_scans (app_id, ran_at DESC);
CREATE INDEX app_scans_spec_idx ON app_scans (spec_id, ran_at DESC);

-- Append-only, for the same reason spec revisions are (R-152, R-319): "why did
-- my app stop" has to be answerable afterwards, and an answer that can be
-- edited after the fact answers nothing. Deletion stays available for the app's
-- own cascade.
CREATE FUNCTION reject_app_scan_update() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'scans are append-only; a new scan is a new row (R-319)'
        USING ERRCODE = 'insufficient_privilege';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER app_scans_append_only
    BEFORE UPDATE ON app_scans
    FOR EACH ROW EXECUTE FUNCTION reject_app_scan_update();

-- When this app was first found below the installation's threshold, and still
-- is. The grace period in host policy is measured from here (R-316).
--
-- On the app rather than in the scans table, because it is a property of the
-- app's relationship to the current policy rather than of any one scan: the
-- score did not change when the threshold did, and this did.
ALTER TABLE apps ADD COLUMN insecure_since timestamptz;

-- Whether Pando stopped this app for being insecure, as opposed to somebody
-- stopping it. The difference decides whether a recovered score starts it again
-- (R-316): an app its owner stopped stays stopped.
ALTER TABLE apps ADD COLUMN stopped_for_security boolean NOT NULL DEFAULT false;
