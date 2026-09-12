DELETE FROM adapter_configs WHERE category = 'backup';
ALTER TABLE adapter_configs DROP CONSTRAINT adapter_configs_category_check;
ALTER TABLE adapter_configs ADD CONSTRAINT adapter_configs_category_check
    CHECK (category IN ('runtime', 'routing', 'builder', 'secrets',
                        'services', 'identity', 'notify'));

DROP TABLE IF EXISTS backups;

ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;
UPDATE roles
SET verbs = array_remove(verbs, 'install.backup.manage'::text)
WHERE id = 'role_administrator';
ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;
