-- Fails if more than one Pando-made group exists, which is the state the
-- up migration exists to allow; delete all but one first.
DROP INDEX groups_native_name_unique;
DROP INDEX groups_synced_unique;
ALTER TABLE groups ADD CONSTRAINT groups_adapter_id_external_id_key UNIQUE NULLS NOT DISTINCT (adapter_id, external_id);
