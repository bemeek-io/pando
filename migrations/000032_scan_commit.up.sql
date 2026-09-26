-- The commit a scan read (R-310, R-312).
--
-- A deploy scanned the source again even when detection had just scanned that
-- same commit minutes earlier, while the person watched. What was scanned is
-- the commit, not the revision: a spec edit that changes a variable does not
-- change a line of the source, and a new commit does. With the commit on the
-- row, a deploy uses the scan of the commit it is deploying and scans only
-- when there is none — the source changed, or it was never scanned.
--
-- Nullable, and null on every row written before this: those scans are of an
-- unknown commit, so they cover none, and the next deploy scans as before.
ALTER TABLE app_scans ADD COLUMN commit text;

CREATE INDEX app_scans_commit ON app_scans (app_id, commit, ran_at DESC) WHERE commit IS NOT NULL;
