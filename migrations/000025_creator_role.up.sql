-- The Creator: a built-in install role for someone who makes their own apps and
-- administers nothing else (R-081).
--
-- One verb, app.create. Everything else follows from the grant model rather
-- than being listed here: whoever creates an app is written its owner (R-073),
-- so a Creator manages the apps they made — and no others, and no users,
-- policy, adapters, backups or audit log. It is the role an installation
-- gives most people who deploy, which is why it ships instead of every
-- operator composing the same one-verb custom role.
--
-- An insert, not an update: the builtin trigger guards UPDATE and DELETE, and
-- a new row is exactly how R-081 says the set of built-ins grows.
INSERT INTO roles (id, name, builtin, scope, verbs) VALUES
    ('role_creator', 'creator', true, 'install', ARRAY['app.create']);
