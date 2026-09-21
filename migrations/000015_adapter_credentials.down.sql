ALTER TABLE adapter_configs DROP CONSTRAINT IF EXISTS adapter_configs_no_inline_credentials;
DROP TABLE IF EXISTS adapter_credentials;
DELETE FROM adapter_configs WHERE category = 'ai';
ALTER TABLE adapter_configs DROP CONSTRAINT adapter_configs_category_check;
ALTER TABLE adapter_configs ADD CONSTRAINT adapter_configs_category_check
    CHECK (category IN ('runtime', 'routing', 'builder', 'secrets',
                        'services', 'identity', 'notify', 'backup'));
