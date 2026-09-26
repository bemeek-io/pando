-- Which AI adapter performs each AI function (R-259, issue #74).
--
-- Before this, one AI adapter did everything: the category default, or the
-- first one configured. An install with two providers could not send plan
-- repair to one and audit search to the other.

-- One AI adapter per provider. Two Anthropic adapters could differ only by
-- credential or model, and an assignment's model covers the second. Checked
-- first so an install that has two says which, rather than failing on an
-- index name.
DO $$
DECLARE dup text;
BEGIN
    SELECT kind INTO dup FROM adapter_configs
    WHERE category = 'ai' GROUP BY kind HAVING count(*) > 1 LIMIT 1;
    IF dup IS NOT NULL THEN
        RAISE EXCEPTION 'This installation has more than one AI adapter of kind %. Pando now allows one AI adapter per provider. Remove all but one of them from adapter_configs (SELECT id FROM adapter_configs WHERE category = ''ai'' AND kind = ''%'') and start Pando again.', dup, dup;
    END IF;
END $$;

CREATE UNIQUE INDEX adapter_configs_one_ai_adapter_per_kind
    ON adapter_configs (kind) WHERE category = 'ai';

-- One row per function, so a function has at most one adapter: the primary
-- key is the uniqueness the requirement asks for, enforced here rather than in
-- a handler. An adapter may hold any number of rows.
--
-- adapter_id has no foreign key, deliberately. An adapter declared in the
-- startup configuration (R-271) has no row in adapter_configs, and may still
-- be assigned a function from the console. Core checks the adapter is a
-- running AI adapter that advertises the function before it writes a row; an
-- assignment whose adapter has since gone leaves the function off (R-335).
CREATE TABLE ai_assignments (
    function   text PRIMARY KEY CHECK (function ~ '^[a-z][a-z_]*$'),
    adapter_id text NOT NULL CHECK (adapter_id <> ''),

    -- Empty is the adapter's own model.
    model      text NOT NULL DEFAULT '',

    updated_by text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- An install that was screening keeps screening. The one AI adapter it had —
-- the default, else the first — keeps the three functions it performed,
-- unless it was set not to (screen_plans: false). The administrative
-- functions are new, and send new things to a provider, so they start off.
INSERT INTO ai_assignments (function, adapter_id, updated_by)
SELECT f, a.id, 'system'
FROM (
    SELECT id FROM adapter_configs
    WHERE category = 'ai' AND enabled
      AND coalesce(config->>'screen_plans', 'true') <> 'false'
    ORDER BY is_default DESC, id
    LIMIT 1
) a
CROSS JOIN unnest(ARRAY['repair_plan', 'answer_questions', 'revise_plan']) AS f;
