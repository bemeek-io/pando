-- Reconciler bookkeeping (design 05 §2).

-- What a deployment actually ran.
--
-- The reconciler restores a workload that has gone missing, and to do that it
-- needs the image — it does not rebuild, because rebuilding to correct drift
-- would turn "someone killed a container" into "ship whatever is on the branch
-- now", which is a different and much larger action than the one being
-- corrected (R-120: a redeploy of a revision builds the same code).
ALTER TABLE deployments ADD COLUMN image_ref text;

-- Failure bookkeeping for R-149's backoff and R-150's give-up threshold.
--
-- On the app rather than in a side table: there is exactly one reconciliation
-- in flight per app, so this is a property of the app, and a join to read it on
-- every tick would be a join for nothing.
ALTER TABLE apps ADD COLUMN consecutive_failures integer NOT NULL DEFAULT 0;

-- When the app last failed to come up.
--
-- R-150 counts failures "within 30 minutes", and the window has to be measured
-- from the *last* failure rather than the first. Measured from the first, the
-- threshold is unreachable: R-149's backoff caps at five minutes, so ten
-- attempts take 5+15+60+300x6 seconds — 31.3 minutes — and the window resets at
-- 30. The app would retry forever and never be given up on, which is the exact
-- outcome R-150 exists to prevent.
--
-- As an idle timeout it does what the requirement means: consecutive failures
-- always reach the threshold however long backoff stretches them, and unrelated
-- failures a day apart never accumulate.
ALTER TABLE apps ADD COLUMN last_failure_at timestamptz;

-- Backoff. The loop skips an app until this passes, so backoff costs nothing
-- per tick and does not need a timer per app.
ALTER TABLE apps ADD COLUMN next_attempt_at timestamptz;

-- The last thing that went wrong, for the console to show beside a degraded
-- app. Cleared when the app reaches running.
ALTER TABLE apps ADD COLUMN last_reconcile_error text;

CREATE INDEX apps_reconcile_idx ON apps (state)
    WHERE deleted_at IS NULL;
