-- The scanner adapter category (R-317).
--
-- Separate from 000015, which created the scan tables, because that migration
-- had already been applied on a running installation by the time this was
-- found — and golang-migrate does not re-run an edited file. The finding was a
-- crash loop: seeding the default scanner on start failed the CHECK, the
-- process exited, Compose restarted it, and it failed again.
--
-- The database is where "these are the categories" is enforced (000005,
-- extended in 000010 for backups), so a category that exists only in Go is a
-- category no adapter can be configured in.
ALTER TABLE adapter_configs DROP CONSTRAINT adapter_configs_category_check;
ALTER TABLE adapter_configs ADD CONSTRAINT adapter_configs_category_check
    CHECK (category IN ('runtime', 'routing', 'builder', 'secrets',
                        'services', 'identity', 'notify', 'backup', 'scanner'));
