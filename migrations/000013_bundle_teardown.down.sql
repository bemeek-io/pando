DROP INDEX IF EXISTS apps_awaiting_teardown_idx;
ALTER TABLE apps DROP COLUMN IF EXISTS bundle_destroyed_at;
