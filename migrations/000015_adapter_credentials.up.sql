-- The ninth adapter category (R-258). A category that exists only in Go is a
-- category no adapter can be configured in, which is what 000010 found for
-- backup and what this finds for ai.
ALTER TABLE adapter_configs DROP CONSTRAINT adapter_configs_category_check;
ALTER TABLE adapter_configs ADD CONSTRAINT adapter_configs_category_check
    CHECK (category IN ('runtime', 'routing', 'builder', 'secrets',
                        'services', 'identity', 'notify', 'backup', 'ai'));

-- Adapter credentials (R-190, R-194; resolves O-20).
--
-- adapter_configs.config is plain jsonb, exported readably into every DR
-- bundle, and was never meant to hold a secret. A credential lives here
-- instead, as ciphertext the install's secrets adapter produced, exactly as an
-- app secret does in `secrets`: there is no plaintext column. Core decrypts it
-- at startup and hands it to the adapter in memory.
CREATE TABLE adapter_credentials (
    adapter_id   text NOT NULL REFERENCES adapter_configs(id) ON DELETE CASCADE,
    field        text NOT NULL CHECK (field ~ '^[a-z][a-z0-9_]*$'),
    adapter_ref  text NOT NULL,     -- the secrets adapter that sealed it
    ciphertext   bytea,
    external_ref text,
    version      integer NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (adapter_id, field),
    CHECK (ciphertext IS NOT NULL OR external_ref IS NOT NULL)
);

-- The only way a credential reaches an adapter is the `credentials` object core
-- assembles from the table above. Refusing that key in config means a request
-- that tries to smuggle one into the plain column fails at the database, not
-- only in a handler somebody might later route around.
ALTER TABLE adapter_configs ADD CONSTRAINT adapter_configs_no_inline_credentials
    CHECK (NOT (config ? 'credentials'));
