-- Where each app is reached, as columns on the app rather than only inside
-- its pinned spec's JSON (R-165, R-167, design 03 §4.1).
--
-- Two reasons. An address has to belong to one app: nothing stopped two apps
-- from pinning the same hostname, and the proxy then answered for whichever
-- the database returned first. And a path is matched by prefix — the longest
-- one that the request's path starts with — which is a query over columns,
-- not over every pinned revision's body.
--
-- Written when a revision is pinned (state.Apps.Pin), in the same
-- transaction, so the column and the pinned spec cannot disagree.
ALTER TABLE apps
    ADD COLUMN address_hostname text,
    ADD COLUMN address_path     text;

-- From what is pinned now. Where two live apps already share an address, the
-- older keeps it and the others are left without one here, so the unique
-- indexes below can be built. The others still resolve as before through
-- their slug, and their next pin says the address is taken.
WITH pinned AS (
    SELECT a.id, a.created_at,
           r.body->'routing'->>'mode'        AS mode,
           lower(r.body->'routing'->>'hostname')    AS hostname,
           lower(r.body->'routing'->>'path_prefix') AS path
    FROM apps a
    JOIN spec_revisions r ON r.id = a.pinned_spec_id
    WHERE a.deleted_at IS NULL
),
hosts AS (
    SELECT id, hostname,
           row_number() OVER (PARTITION BY hostname ORDER BY created_at, id) AS n
    FROM pinned WHERE mode = 'subdomain' AND hostname IS NOT NULL AND hostname <> ''
),
paths AS (
    SELECT id, path,
           row_number() OVER (PARTITION BY path ORDER BY created_at, id) AS n
    FROM pinned WHERE mode = 'path' AND path IS NOT NULL AND path <> ''
)
UPDATE apps a
   SET address_hostname = (SELECT h.hostname FROM hosts h WHERE h.id = a.id AND h.n = 1),
       address_path     = (SELECT p.path FROM paths p WHERE p.id = a.id AND p.n = 1)
 WHERE a.deleted_at IS NULL;

-- One live app per address. An archived app keeps its row (deletion archives)
-- and gives its address up.
CREATE UNIQUE INDEX apps_address_hostname_unique
    ON apps (address_hostname) WHERE deleted_at IS NULL AND address_hostname IS NOT NULL;
CREATE UNIQUE INDEX apps_address_path_unique
    ON apps (address_path) WHERE deleted_at IS NULL AND address_path IS NOT NULL;
