-- A Traefik adapter configured before Pando ran its own edge (R-174) is one
-- somebody else runs: until now that was the only kind. A new Traefik adapter
-- defaults to Pando running it, and without this every existing one would too
-- on upgrade — a second Traefik reaching for ports 80 and 443 on a host that
-- already has one on them. Design 03 §4.4.
--
-- updated_at is left alone: this records what was already true, and bumping it
-- would mark the adapter as saved since startup and waiting on a restart.
UPDATE adapter_configs
   SET config = config || '{"managed": false}'::jsonb
 WHERE category = 'routing'
   AND kind = 'traefik'
   AND NOT (config ? 'managed');
