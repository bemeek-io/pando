DROP TABLE IF EXISTS backup_attempts;

-- Refuses, correctly, once two apps hold a volume with the same ID: the old
-- key cannot describe that installation, and picking a row to drop would
-- drop an app's record of its data.
ALTER TABLE volumes DROP CONSTRAINT volumes_pkey;
ALTER TABLE volumes ADD PRIMARY KEY (id);
