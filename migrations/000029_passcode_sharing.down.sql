DROP TABLE IF EXISTS passcode_unlocks;
ALTER TABLE grants DROP CONSTRAINT IF EXISTS grants_passcode_is_anonymous_use;
ALTER TABLE grants DROP COLUMN IF EXISTS passcode_hash;
