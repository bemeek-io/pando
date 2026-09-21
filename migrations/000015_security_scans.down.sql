ALTER TABLE apps DROP COLUMN IF EXISTS stopped_for_security;
ALTER TABLE apps DROP COLUMN IF EXISTS insecure_since;

DROP TRIGGER IF EXISTS app_scans_append_only ON app_scans;
DROP FUNCTION IF EXISTS reject_app_scan_update();
DROP TABLE IF EXISTS app_scans;
