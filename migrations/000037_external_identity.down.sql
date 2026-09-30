DROP VIEW IF EXISTS effective_group_members;
DROP TRIGGER IF EXISTS group_links_direction ON group_links;
DROP FUNCTION IF EXISTS group_links_direction();
DROP TABLE IF EXISTS group_links;
ALTER TABLE groups DROP COLUMN IF EXISTS scim_resource, DROP COLUMN IF EXISTS updated_at;

DROP TABLE IF EXISTS sso_replay;
DROP TABLE IF EXISTS sso_flows;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_alias_not_self,
    DROP CONSTRAINT IF EXISTS users_alias_is_suspended,
    DROP COLUMN IF EXISTS suspended_by,
    DROP COLUMN IF EXISTS alias_of;

DROP TABLE IF EXISTS user_identities;
DROP TABLE IF EXISTS identity_adapter_credentials;

DROP INDEX IF EXISTS identity_adapters_scim_token_unique;
DROP INDEX IF EXISTS identity_adapters_name_unique;
ALTER TABLE identity_adapters
    DROP COLUMN IF EXISTS link_by_email,
    DROP COLUMN IF EXISTS jit_provisioning,
    DROP COLUMN IF EXISTS scim_identity_attribute,
    DROP COLUMN IF EXISTS scim_token_created_at,
    DROP COLUMN IF EXISTS scim_token_hash,
    DROP CONSTRAINT IF EXISTS identity_adapters_no_inline_credentials,
    DROP CONSTRAINT IF EXISTS identity_adapters_kind_check;
