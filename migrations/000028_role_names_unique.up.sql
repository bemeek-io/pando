-- Role names are unique however they are written (R-082).
--
-- roles.name was UNIQUE, but case-sensitively and with surrounding spaces
-- counted: the built-ins are stored lowercase and shown capitalized, so a
-- custom role called "Administrator" or "viewer " was accepted and appeared
-- in every role picker beside the real one, looking identical.
--
-- Any existing collision is renamed first, so this migration cannot fail on an
-- installation that already has one: a custom role clashing with a built-in,
-- or with an older custom role, gets " (custom)" and, if that is taken too,
-- its ID. Built-in names never change here (R-081).
UPDATE roles r
SET name = btrim(r.name) || ' (custom)'
WHERE NOT r.builtin
  AND EXISTS (
    SELECT 1 FROM roles o
    WHERE o.id <> r.id
      AND lower(btrim(o.name)) = lower(btrim(r.name))
      AND (o.builtin OR o.created_at < r.created_at OR (o.created_at = r.created_at AND o.id < r.id))
  );

UPDATE roles r
SET name = btrim(r.name) || ' ' || r.id
WHERE NOT r.builtin
  AND EXISTS (
    SELECT 1 FROM roles o
    WHERE o.id <> r.id
      AND lower(btrim(o.name)) = lower(btrim(r.name))
      AND (o.builtin OR o.created_at < r.created_at OR (o.created_at = r.created_at AND o.id < r.id))
  );

CREATE UNIQUE INDEX roles_name_folded_unique ON roles (lower(btrim(name)));
