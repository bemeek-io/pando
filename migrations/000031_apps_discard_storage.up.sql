-- A deleted app's storage goes with it once the delete has settled what
-- becomes of it (R-204): discarded with force=true, or backed up first with
-- backup=true. The delete removes the volume rows as it always has, so the
-- decision is recorded on the app, where the GC's teardown reads it and has the
-- runtime destroy the volumes along with the bundle.
--
-- It had been kept instead, forever: the volumes stayed on disk after a delete
-- that said to discard them, with no row left through which Pando could reach
-- or reclaim them (issue #55, R-224).
ALTER TABLE apps ADD COLUMN discard_storage boolean NOT NULL DEFAULT false;
