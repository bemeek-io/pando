-- Identity and principals (design 02 §2.1, §2.7).
--
-- Identity adapters authenticate only (R-044). Nothing in this file records what
-- anyone is allowed to do — that is 000003, and the separation is the point.

CREATE TABLE identity_adapters (
    id          text PRIMARY KEY,
    kind        text NOT NULL,               -- local | oidc | saml | github
    name        text NOT NULL,
    config      jsonb NOT NULL DEFAULT '{}',
    enabled     boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            text PRIMARY KEY,
    adapter_id    text NOT NULL REFERENCES identity_adapters(id),

    -- The subject as the adapter knows them. users.id is what the rest of the
    -- system uses, and what goes in the assertion sub claim (R-054) — stable
    -- across an email change and independent of the adapter's own identifiers.
    -- Apps key their data on it, which makes it the most important stability
    -- guarantee in the schema.
    external_id   text NOT NULL,

    email         text,
    display_name  text,

    -- Three-valued deliberately: suspended is not deleted (R-049, R-282).
    -- Destruction rules (R-280) fire on 'deleted', never on 'suspended'.
    status        text NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active', 'suspended', 'deleted')),

    password_hash text,                      -- local adapter only, argon2id
    must_change_password boolean NOT NULL DEFAULT false,

    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,

    UNIQUE (adapter_id, external_id)
);

CREATE INDEX users_email_idx ON users (lower(email)) WHERE deleted_at IS NULL;

CREATE TABLE groups (
    id          text PRIMARY KEY,
    adapter_id  text REFERENCES identity_adapters(id),  -- NULL = Pando-native
    external_id text,
    name        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (adapter_id, external_id)
);

CREATE TABLE group_members (
    group_id text NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id  text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);

-- Membership is read live at authorization time (R-079), never denormalized
-- into grants, so this index is on the hot path.
CREATE INDEX group_members_user_idx ON group_members (user_id);

CREATE TABLE tokens (
    id            text PRIMARY KEY,
    kind          text NOT NULL CHECK (kind IN ('delegated', 'account')),
    name          text NOT NULL,
    hash          text NOT NULL,             -- argon2id; the secret is shown once (R-063)

    -- A delegated token acts as its owner and holds no grants of its own
    -- (R-058, R-059). An account token is its own principal and appears
    -- directly in grants.principal_id (R-060).
    owner_user_id text REFERENCES users(id),

    created_by    text NOT NULL,
    expires_at    timestamptz,               -- NULL = never; policy may forbid (R-061)
    last_used_at  timestamptz,               -- R-062
    revoked_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tokens_delegated_has_owner CHECK (
        (kind = 'delegated' AND owner_user_id IS NOT NULL) OR
        (kind = 'account'   AND owner_user_id IS NULL)
    )
);

CREATE INDEX tokens_owner_idx ON tokens (owner_user_id) WHERE revoked_at IS NULL;

CREATE TABLE sessions (
    id         text PRIMARY KEY,
    user_id    text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    adapter_id text NOT NULL REFERENCES identity_adapters(id),
    issued_at  timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    user_agent text,
    ip         inet
);

-- Sessions are server-side rows, not stateless cookies: the cookie carries only
-- ses_…. Revocation has to be immediate when an adapter can push (R-048), and a
-- stateless cookie cannot do that.
CREATE INDEX sessions_user_idx ON sessions (user_id) WHERE revoked_at IS NULL;
