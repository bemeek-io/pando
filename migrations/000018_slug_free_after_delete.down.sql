DROP INDEX IF EXISTS apps_slug_live_key;

-- Restoring the constraint can fail where a slug has been reused since, which
-- is the point of the change. A down migration that cannot run on real data is
-- better than one that silently drops a row to make itself run.
ALTER TABLE apps ADD CONSTRAINT apps_slug_key UNIQUE (slug);
