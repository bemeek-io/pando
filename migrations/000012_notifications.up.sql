-- Console notifications (R-230, R-231).
--
-- V1 is console-only: a notification is recorded here and shown the next time
-- the recipient looks at Pando. Nothing is sent anywhere.
--
-- The honest consequence, stated where someone will read it: this reaches
-- nobody who is not already looking. That is exactly why R-266 says sharing an
-- app sends no message — the launcher tile is a better notification, because it
-- waits for someone who has never signed in. R-232's SMTP adapter is what
-- changes that.
CREATE TABLE notifications (
    id          text PRIMARY KEY,              -- ntf_...
    user_id     text NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- NULL when the notification is not about one app.
    app_id      text REFERENCES apps(id) ON DELETE CASCADE,

    kind        text NOT NULL,
    subject     text NOT NULL,
    body        text NOT NULL DEFAULT '',

    read_at     timestamptz,
    retain_until timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- The unread-badge query, and the sweep.
CREATE INDEX notifications_unread_idx ON notifications (user_id, created_at DESC)
    WHERE read_at IS NULL;
CREATE INDEX notifications_retention_idx ON notifications (retain_until)
    WHERE retain_until IS NOT NULL;

-- ON DELETE CASCADE on user_id, unlike backups: a notification is a message to
-- a person, and a message to a person who no longer exists is not a record
-- worth keeping. The audit log is where "what happened" lives (R-227), and it
-- does not cascade.
