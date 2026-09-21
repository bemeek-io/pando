-- A deleted app does not hold its name (R-204, R-049).
--
-- `slug` was UNIQUE across every row, and a deleted app keeps its row: the
-- archive is what a backup taken on delete refers to, what the audit log's
-- target ID resolves against, and what the reconciler tears the bundle down
-- from. So deleting `crewmate` and adding it again answered "an app named
-- crewmate already exists" — pointing at a row the person had just deleted and
-- can no longer see.
--
-- Partial, on the live rows only. Two live apps still cannot share a slug,
-- which is what the constraint was for: a slug is an address (design 03 §4.2),
-- and two apps at one address is the failure it prevents. An archived app is at
-- no address at all — its routing is removed and its bundle destroyed — so it
-- has no claim on one.
--
-- NULLS NOT DISTINCT is deliberately absent: `deleted_at` is the predicate, not
-- a column in the index.
ALTER TABLE apps DROP CONSTRAINT apps_slug_key;

CREATE UNIQUE INDEX apps_slug_live_key ON apps (slug) WHERE deleted_at IS NULL;
