-- The image on an app's launcher tile (R-340).
--
-- A table of its own rather than a column on apps: every list of apps reads
-- that table, and none of them wants the bytes — only whether there is an
-- image and when it changed, so a browser can tell a new one from the one it
-- has cached.
--
-- Presentation, not configuration. The spec is the record of how an app runs
-- (R-020); what its tile looks like is not part of that, so this is not in the
-- spec and a rollback does not change it.
--
-- In Postgres rather than on disk because it is small (capped at 256 KiB by
-- the API) and is then covered by the same backups as the app row it belongs
-- to. ON DELETE CASCADE: apps soft-delete, so this only fires if an app row is
-- purged, and an image with no app is nothing worth keeping.
CREATE TABLE app_icons (
    app_id       text PRIMARY KEY REFERENCES apps (id) ON DELETE CASCADE,
    content_type text NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp', 'image/gif')),
    data         bytea NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
