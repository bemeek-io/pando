-- Host ports handed to apps in port-mode routing (O-15).

-- Ports were first derived by scanning pinned specs and detection drafts for
-- ports already in use. That is not an allocation, it is a guess about one, and
-- it raced the moment two apps were added at once: detection runs in the
-- background per app, and three apps created together produced two holding the
-- same port. Nothing detected the collision, because nothing was keeping track.
--
-- A row with a unique constraint cannot do that. The database refuses the second
-- writer rather than both believing they won — the same reasoning as every other
-- invariant Pando enforces here rather than in code.
CREATE TABLE port_allocations (
    adapter_ref text    NOT NULL,
    port        integer NOT NULL CHECK (port > 0 AND port < 65536),
    app_id      text    NOT NULL REFERENCES apps(id) ON DELETE CASCADE,

    allocated_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (adapter_ref, port),

    -- One port per app per adapter. Re-detecting an app reuses the port it
    -- already holds rather than consuming another and leaving a bookmark
    -- pointing at nothing.
    UNIQUE (adapter_ref, app_id)
);

CREATE INDEX port_allocations_app_idx ON port_allocations (app_id);
