-- Configured adapter instances and host policy (design 02 §2.5).

CREATE TABLE adapter_configs (
    id         text PRIMARY KEY,
    category   text NOT NULL
               CHECK (category IN ('runtime', 'routing', 'builder', 'secrets', 'services', 'identity', 'notify')),
    kind       text NOT NULL,
    name       text NOT NULL,
    config     jsonb NOT NULL DEFAULT '{}',
    is_default boolean NOT NULL DEFAULT false,
    enabled    boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One default per category. A second default is not a preference to resolve at
-- read time — it is a configuration mistake, and the index says so at write time.
CREATE UNIQUE INDEX adapter_configs_one_default_per_category
    ON adapter_configs (category) WHERE is_default;

-- Host policy is a singleton by constraint (R-015).
--
-- One install serves one organization. Encoding that here means nobody
-- accidentally builds multi-tenancy on top of a policy table that quietly
-- allowed more than one row.
CREATE TABLE host_policy (
    id         integer PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    body       jsonb NOT NULL,
    updated_by text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Permissive defaults (R-270). An install starts usable and is tightened
-- deliberately, rather than starting locked and being loosened by whoever hits
-- the first wall.
INSERT INTO host_policy (id, body, updated_by) VALUES (
    1,
    '{"allow_anonymous_grants": true, "min_build_isolation": 10, "min_runtime_isolation": 10}'::jsonb,
    'system'
);
