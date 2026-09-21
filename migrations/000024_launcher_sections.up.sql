-- Sections: groupings a person makes in their own launcher (R-342).
--
-- Per account, like favorites, and for the same reason: how somebody arranges
-- the apps they use is a fact about them, not about the browser they are in.
-- A section grants nothing; the launcher is still scoped by data-plane grants,
-- so an app placed in a section and later unshared simply stops appearing.
--
-- An app sits in at most one of a person's sections — the primary key on
-- placements says so — and anything not placed is under "Your apps".
--
-- The composite foreign key is the structural guarantee that a placement's
-- section belongs to the same person as the placement: nobody can file an app
-- into someone else's section, whatever the handler does.
CREATE TABLE launcher_sections (
    id         text PRIMARY KEY,
    user_id    text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, name),
    UNIQUE (id, user_id)
);

-- No updated_at: moving an app replaces the row's section, and that is the
-- whole of its life.
CREATE TABLE launcher_placements (
    user_id    text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    app_id     text NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    section_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, app_id),
    -- Deleting a section returns its apps to "Your apps".
    FOREIGN KEY (section_id, user_id) REFERENCES launcher_sections (id, user_id) ON DELETE CASCADE
);
