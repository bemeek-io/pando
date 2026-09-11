DROP TABLE IF EXISTS secrets;
DROP TABLE IF EXISTS volumes;
DROP TABLE IF EXISTS spec_pins;
DROP TABLE IF EXISTS deployments;
ALTER TABLE apps DROP COLUMN IF EXISTS pinned_spec_id;
DROP TRIGGER IF EXISTS spec_revisions_append_only ON spec_revisions;
DROP FUNCTION IF EXISTS reject_spec_revision_update();
DROP TABLE IF EXISTS spec_revisions;
