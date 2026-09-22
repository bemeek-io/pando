-- Apps a person has marked as favorites, shown first in their launcher (R-341).
--
-- Per account rather than per browser, unlike the theme: which apps somebody
-- reaches for is a fact about them, and they should find the same launcher on
-- a new laptop.
--
-- A favorite grants nothing. It is read only through GET /me/apps, which is
-- already scoped to the apps the person can open, so a favorite on an app they
-- have since lost access to is simply not shown — no cascade to write when a
-- grant is revoked, and nothing to leak.
--
-- No updated_at: a row is created or deleted, never changed.
CREATE TABLE app_favorites (
    user_id    text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    app_id     text NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, app_id)
);
