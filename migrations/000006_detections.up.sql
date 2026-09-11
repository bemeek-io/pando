-- Detection proposals (Sequence A, design 04 §2.2).

-- One row per app: the current auction result.
--
-- R-098 and R-022: detection does not re-run implicitly, and a re-run is an
-- explicit act that replaces what was there. There is no history to keep,
-- because the thing worth keeping is the pinned spec, and that lives in
-- spec_revisions where it is append-only.
CREATE TABLE detections (
    app_id     text PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,

    status     text NOT NULL
               CHECK (status IN ('running', 'ready', 'needs_answers', 'unknown', 'blocked', 'failed')),

    -- The whole proposal: winning bid, runners-up, questions, draft spec,
    -- warnings. Stored as one document because it is read as one — the console
    -- shows the auction, not a join of its parts (R-093, R-102).
    body       jsonb NOT NULL DEFAULT '{}'::jsonb,

    -- Answers keyed by question key, as supplied by the user (R-105's workflow
    -- is pasting a question into an assistant and pasting the answer back).
    -- Kept beside the proposal rather than folded into it, so re-running
    -- detection can re-apply what was already answered instead of asking again.
    answers    jsonb NOT NULL DEFAULT '{}'::jsonb,

    -- R-120: Ref is what the user asked for, Commit is what was read.
    commit     text,

    started_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX detections_status_idx ON detections (status);

-- Where the app comes from.
--
-- Until now this was accepted at app creation, checked against the source
-- allowlist (R-092), and then dropped — there was nothing to detect with, so
-- nothing needed it. Detection does: the job clones this, and an app created
-- from a git URL has that URL as a property of itself well before any spec
-- exists to carry it.
--
-- It lives here rather than as columns because spec.Source is one value with
-- several shapes (git with a ref and a subdir, or an image with a digest), and
-- splitting it into columns would put the shape rules in two places.
ALTER TABLE apps ADD COLUMN source jsonb NOT NULL DEFAULT '{}'::jsonb;
