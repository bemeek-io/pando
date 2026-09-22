-- An installation could hold only one Pando-made group (R-078).
--
-- 000002 made (adapter_id, external_id) unique with NULLS NOT DISTINCT, so that
-- a group synced from an identity provider appears once. A group made in Pando
-- has neither — both NULL — and NULLS NOT DISTINCT counts every such pair as
-- the same one: the second group anyone made was refused as a duplicate of the
-- first, whatever its name, with a message saying a group by that name already
-- existed.
--
-- The two rules it was standing in for, stated separately: a synced group is
-- unique by where it came from, and a Pando-made group by its name — which is
-- what the error message always claimed.
ALTER TABLE groups DROP CONSTRAINT groups_adapter_id_external_id_key;

CREATE UNIQUE INDEX groups_synced_unique ON groups (adapter_id, external_id) WHERE adapter_id IS NOT NULL;
CREATE UNIQUE INDEX groups_native_name_unique ON groups (name) WHERE adapter_id IS NULL;
