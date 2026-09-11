-- Audit events. Append-only, enforced by database grant rather than by code
-- review (R-027, design 02 §2.6).
--
-- The grants themselves are applied by internal/core/state at startup, not here,
-- so that the policy lives in one place and a future migration cannot silently
-- create a table that escapes it. See state.applyGrants.

CREATE TABLE audit_events (
    id             bigserial PRIMARY KEY,
    occurred_at    timestamptz NOT NULL DEFAULT now(),

    -- Who. A delegated token records both the token and the user it acts for,
    -- so R-229's "on behalf of" is answerable without a join to a mutable row.
    principal_kind text NOT NULL CHECK (principal_kind IN ('user', 'token', 'system', 'anonymous')),
    principal_id   text,
    on_behalf_of   text,

    -- What.
    action         text NOT NULL,
    app_id         text,
    target_kind    text,
    target_id      text,

    request_id     text,

    -- Never contains a secret value. secret.Value makes that structural on the
    -- Go side; nothing here can enforce it, which is why the type exists.
    detail         jsonb NOT NULL DEFAULT '{}'
);

-- An anonymous principal has no id; every other kind must have one.
ALTER TABLE audit_events ADD CONSTRAINT audit_events_principal_id_present
    CHECK (principal_kind = 'anonymous' OR principal_id IS NOT NULL);

-- The two questions the audit log is asked: what happened to this app, and what
-- did this principal do.
CREATE INDEX audit_events_app_idx ON audit_events (app_id, occurred_at DESC);
CREATE INDEX audit_events_principal_idx ON audit_events (principal_id, occurred_at DESC);
CREATE INDEX audit_events_action_idx ON audit_events (action, occurred_at DESC);
