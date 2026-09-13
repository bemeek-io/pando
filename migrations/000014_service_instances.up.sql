-- Provisioned services (R-131 "provisioned", R-134, R-135).
--
-- One row per filled slot. The row is the only durable record that an app has a
-- database inside its bundle: the spec says the slot is provisioned, but not
-- which instance filled it, and without that a redeploy would stand up a second
-- database and connect the app to the empty one.
CREATE TABLE service_instances (
    id          text PRIMARY KEY,
    app_id      text NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    slot_key    text NOT NULL,
    slot_type   text NOT NULL,
    adapter_ref text NOT NULL,
    handle      text NOT NULL,

    -- The app secret holding the connection string. Resolved into the
    -- workload's environment at deploy, exactly like any other secret, so the
    -- runtime never learns a service was provisioned rather than bound.
    secret_key  text NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now(),

    -- One instance per slot. R-134: a provisioned service belongs to one app
    -- and is not shared, and this is the half of that claim the database can
    -- hold on its own.
    UNIQUE (app_id, slot_key)
);

CREATE INDEX service_instances_app_idx ON service_instances (app_id);
