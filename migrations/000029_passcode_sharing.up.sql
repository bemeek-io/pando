-- Public with a passcode (R-075a).
--
-- The anonymous grant (R-074, R-075) may carry a passcode: the app is still
-- shared with everyone, but a visitor enters the passcode before the proxy lets
-- them through. Only the argon2id digest is stored, and only on the one kind
-- of grant it means anything on — the database refuses it anywhere else, so a
-- passcode cannot end up on a person's grant or on a grant to manage an app.
ALTER TABLE grants ADD COLUMN passcode_hash text;
ALTER TABLE grants ADD CONSTRAINT grants_passcode_is_anonymous_use
    CHECK (passcode_hash IS NULL OR (principal_kind = 'anonymous' AND plane = 'data'));

-- A visitor who entered the passcode. The browser holds a random token in a
-- cookie in Pando's namespace (stripped before any request reaches the app,
-- R-173); this holds its SHA-256, the app, and the grant it unlocked.
--
-- Tied to the grant, not only the app: making the app private deletes the
-- grant and every unlock with it, and changing the passcode deletes them in the
-- same statement (state.Grants.SetPasscode) — a new passcode is not a new
-- passcode if the people who knew the old one are still let in.
CREATE TABLE passcode_unlocks (
    token_hash text PRIMARY KEY,
    app_id     text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    grant_id   text NOT NULL REFERENCES grants(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX passcode_unlocks_grant_idx ON passcode_unlocks (grant_id);
