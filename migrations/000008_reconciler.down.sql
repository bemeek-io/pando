DROP INDEX IF EXISTS apps_reconcile_idx;
ALTER TABLE apps DROP COLUMN IF EXISTS last_reconcile_error;
ALTER TABLE apps DROP COLUMN IF EXISTS next_attempt_at;
ALTER TABLE apps DROP COLUMN IF EXISTS last_failure_at;
ALTER TABLE apps DROP COLUMN IF EXISTS consecutive_failures;
ALTER TABLE deployments DROP COLUMN IF EXISTS image_ref;
