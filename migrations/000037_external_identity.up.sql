-- External identity: OIDC and SAML sign-in, SCIM provisioning (issue #51;
-- R-043, R-045, R-047, R-048, R-049, R-050).
--
-- identity_adapters has existed since 000002 with one row in it. This makes it
-- able to hold the rest: a provider an administrator adds in the console, its
-- secret sealed like any adapter's, its SCIM token as a digest, and the
-- identities it vouches for attached to accounts as aliases (O-1).

-- 1. Providers ---------------------------------------------------------------

ALTER TABLE identity_adapters
    ADD CONSTRAINT identity_adapters_kind_check
        CHECK (kind IN ('local', 'oidc', 'saml', 'github')),

    -- The same rule as adapter_configs (R-190, O-20): a secret never sits in
    -- the plain jsonb column, which is exported readably into every DR
    -- bundle. The names an administrator would reach for are refused too, so a
    -- client secret pasted into the wrong field fails at the database instead
    -- of being stored.
    ADD CONSTRAINT identity_adapters_no_inline_credentials
        CHECK (NOT (config ?| ARRAY['credentials', 'client_secret', 'scim_token'])),

    -- The SCIM bearer token, as a SHA-256 digest (R-048). NULL is SCIM off.
    -- A digest rather than argon2id because the token is 256 random bits and
    -- is checked on every push; the slow hash exists for things people choose.
    ADD COLUMN scim_token_hash       text,
    ADD COLUMN scim_token_created_at timestamptz,

    -- Which SCIM attribute holds the value this provider signs people in
    -- with, so a pushed account and a sign-in meet on the same identity.
    -- NULL is the kind's default: externalId for OIDC (Okta sends the user ID
    -- the sub claim carries), userName for SAML (the NameID).
    ADD COLUMN scim_identity_attribute text
        CHECK (scim_identity_attribute IN ('externalId', 'userName')),

    -- Core's settings for the provider, not the adapter's: whether a first
    -- sign-in makes an account (off by default; host policy can refuse it
    -- everywhere), and whether one may be matched to an existing account by
    -- an email the provider vouches for (O-1).
    ADD COLUMN jit_provisioning boolean NOT NULL DEFAULT false,
    ADD COLUMN link_by_email    boolean NOT NULL DEFAULT false;

-- Providers are chosen by name on the sign-in page, so two called "Okta" would
-- be two identical buttons.
CREATE UNIQUE INDEX identity_adapters_name_unique ON identity_adapters (lower(btrim(name)));

CREATE UNIQUE INDEX identity_adapters_scim_token_unique
    ON identity_adapters (scim_token_hash) WHERE scim_token_hash IS NOT NULL;

-- A provider's secrets, sealed by the install's secrets adapter (R-190). The
-- shape of adapter_credentials, one table over, because that table's foreign
-- key is to adapter_configs and an identity provider lives here.
CREATE TABLE identity_adapter_credentials (
    adapter_id   text NOT NULL REFERENCES identity_adapters(id) ON DELETE CASCADE,
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

-- 2. Accounts ----------------------------------------------------------------

-- Linking aliases, it never merges (O-1, design 02 §2.1).
--
-- users.adapter_id and users.external_id stay what R-045 asks for: the
-- provider an account came from. Every identity an external provider vouches
-- for — the originating one and any an administrator links — is a row here,
-- and a sign-in resolves through this table alone. The primary key is the
-- guarantee that matters: one identity reaches one account, whatever linked
-- it.
--
-- Local accounts are not listed. Their identity is their username, which a
-- person may change (users.external_id), and the local adapter resolves it
-- where it always has.
CREATE TABLE user_identities (
    adapter_id  text NOT NULL REFERENCES identity_adapters(id),
    external_id text NOT NULL,
    user_id     text NOT NULL REFERENCES users(id),

    -- What a SCIM client said about this person (R-048), kept so a GET
    -- returns what was PUT. Profile only: nothing in it is read by
    -- authorization, and a client cannot put a role here.
    scim_user_name   text,
    scim_external_id text,
    scim_resource    jsonb,

    last_sign_in_at timestamptz,
    created_by      text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (adapter_id, external_id)
);

CREATE INDEX user_identities_user_idx ON user_identities (user_id);

-- SCIM clients look people up by userName before creating them, and RFC 7643
-- makes it unique and case-insensitive within the provider.
CREATE UNIQUE INDEX user_identities_scim_user_name_unique
    ON user_identities (adapter_id, lower(scim_user_name)) WHERE scim_user_name IS NOT NULL;

INSERT INTO user_identities (adapter_id, external_id, user_id, created_by)
SELECT u.adapter_id, u.external_id, u.id, 'system'
FROM users u
JOIN identity_adapters a ON a.id = u.adapter_id
WHERE a.kind <> 'local' AND u.deleted_at IS NULL
ON CONFLICT DO NOTHING;

ALTER TABLE users
    -- An account whose identities were moved to another by an administrator
    -- (O-1). Never deleted: its ID is an assertion subject apps may hold data
    -- under (R-054), and a deletion would free its external_id for reuse by a
    -- different person.
    ADD COLUMN alias_of text REFERENCES users(id),

    -- Who suspended the account: an administrator, or a SCIM client in the
    -- name of a provider. A SCIM client re-sending active=true — Entra does it
    -- on every sync cycle — lifts only a suspension it made, never an
    -- administrator's (R-049).
    ADD COLUMN suspended_by text,

    ADD CONSTRAINT users_alias_is_suspended
        CHECK (alias_of IS NULL OR status <> 'active'),
    ADD CONSTRAINT users_alias_not_self
        CHECK (alias_of IS NULL OR alias_of <> id);

-- 3. Sign-in flows -------------------------------------------------------------

-- A redirect sign-in in progress (design 06 §3.2).
--
-- Server-side, because a flow crosses hostnames: it can start on an app's own
-- hostname (R-172) and the provider returns to the one callback address
-- registered with it. The browser that started the flow holds a cookie whose
-- digest is bind_hash, and the flow completes only in that browser — without
-- it, anyone could finish a sign-in they started in somebody else's browser
-- (login CSRF).
CREATE TABLE sso_flows (
    id            text PRIMARY KEY,   -- the state parameter: 256 random bits, not a ULID
    adapter_id    text NOT NULL REFERENCES identity_adapters(id) ON DELETE CASCADE,
    purpose       text NOT NULL CHECK (purpose IN ('sign_in', 'test')),
    bind_hash     text,               -- NULL only for an IdP-initiated SAML sign-in
    return_origin text NOT NULL,
    next_path     text NOT NULL DEFAULT '/',
    callback_url  text NOT NULL,
    entity_id     text NOT NULL,

    -- The adapter's own data for this flow — a PKCE verifier and nonce, or a
    -- SAML request ID — handed back to it at the callback. Lives for minutes,
    -- is consumed once, and is worthless without the provider's response.
    flow          bytea,

    initiated_by  text,               -- a test sign-in: the administrator who asked
    user_id       text REFERENCES users(id),
    handoff_hash  text,               -- digest of the one-time code that finishes the flow
    result        jsonb,              -- a test sign-in's report, or why a sign-in failed
    failed        boolean NOT NULL DEFAULT false,

    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    consumed_at   timestamptz
);

CREATE INDEX sso_flows_expires_idx ON sso_flows (expires_at);
CREATE UNIQUE INDEX sso_flows_handoff_idx ON sso_flows (handoff_hash) WHERE handoff_hash IS NOT NULL;

-- One-time identifiers a provider promises are used once — a SAML assertion's
-- ID. Recorded until the assertion would have expired anyway, so a captured
-- response cannot be replayed within its own validity window.
CREATE TABLE sso_replay (
    adapter_id  text NOT NULL REFERENCES identity_adapters(id) ON DELETE CASCADE,
    one_time_id text NOT NULL,
    expires_at  timestamptz NOT NULL,
    PRIMARY KEY (adapter_id, one_time_id)
);

-- 4. Groups --------------------------------------------------------------------

-- A group a SCIM client created keeps what the client said about it.
ALTER TABLE groups
    ADD COLUMN scim_resource jsonb,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- Mapping a provider's group onto a Pando group (R-078).
--
-- Everyone in the synced group counts as a member of the Pando group, live
-- (R-079): the provider still says who is in it, and Pando still says what it
-- can do. A synced group can hold grants directly too; this is for the team
-- that already has a Pando group with the right access.
CREATE TABLE group_links (
    synced_group_id text NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    group_id        text NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_by      text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (synced_group_id, group_id)
);

CREATE INDEX group_links_group_idx ON group_links (group_id);

-- A link runs from a provider's group to a Pando-made one and nowhere else.
-- The other directions would let a provider's membership decide a Pando-made
-- group's, or chain one provider's groups into another's, and neither is
-- something anyone meant.
CREATE FUNCTION group_links_direction() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM groups WHERE id = NEW.synced_group_id AND adapter_id IS NOT NULL) THEN
        RAISE EXCEPTION 'group_links.synced_group_id must be a group synced from an identity provider'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM groups WHERE id = NEW.group_id AND adapter_id IS NULL) THEN
        RAISE EXCEPTION 'group_links.group_id must be a group made in Pando'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER group_links_direction
    BEFORE INSERT OR UPDATE ON group_links
    FOR EACH ROW EXECUTE FUNCTION group_links_direction();

-- Membership as authorization reads it: direct, plus through a link. One
-- definition so that every query asking "which groups is this person in"
-- gives the same answer — the authorizer, the lockout checks and the apps
-- list — rather than each growing its own UNION.
CREATE VIEW effective_group_members AS
    SELECT group_id, user_id FROM group_members
    UNION
    SELECT l.group_id, m.user_id
    FROM group_links l
    JOIN group_members m ON m.group_id = l.synced_group_id;
