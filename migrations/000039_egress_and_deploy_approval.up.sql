-- Egress rules with app-level overrides (issue #79) and deploy approval
-- (issue #39). R-154 – R-159, R-181 – R-189.

-- Verbs (R-080, R-081). Built-in roles change by migration only, the
-- sanctioned path; custom roles that held the old egress verb are renamed with
-- them, because the meaning carried over and silently dropping a grant would
-- take away something an administrator deliberately gave.
ALTER TABLE roles DISABLE TRIGGER roles_builtin_immutable;

-- app.egress.override becomes app.egress.loosen: an app's list used to replace
-- the install's, so defining one was the escalation; now an app's own list
-- narrows, and only loosening is.
UPDATE roles
SET verbs = array_replace(verbs, 'app.egress.override'::text, 'app.egress.loosen'::text)
WHERE 'app.egress.override' = ANY (verbs);

-- app.egress.tighten: changing egress within the install's rules. Owner and
-- Operator; tightening is always within policy (R-272), and Operator already
-- edits the spec.
UPDATE roles
SET verbs = verbs || 'app.egress.tighten'::text
WHERE id IN ('role_owner', 'role_operator');

-- install.deploys.approve: the Administrator's. app.deploy.approve is in no
-- built-in role (R-155).
UPDATE roles
SET verbs = verbs || 'install.deploys.approve'::text
WHERE id = 'role_administrator';

ALTER TABLE roles ENABLE TRIGGER roles_builtin_immutable;

-- Host policy names verbs too: a verb an install disabled, for everyone or for
-- agents, stays disabled under its new name.
UPDATE host_policy
SET body = jsonb_set(body, '{disabled_verbs}',
        (SELECT coalesce(jsonb_agg(CASE WHEN v = 'app.egress.override' THEN 'app.egress.loosen' ELSE v END), '[]'::jsonb)
         FROM jsonb_array_elements_text(body->'disabled_verbs') AS v))
WHERE body->'disabled_verbs' ? 'app.egress.override';

UPDATE host_policy
SET body = jsonb_set(body, '{agent_disabled_verbs}',
        (SELECT coalesce(jsonb_agg(CASE WHEN v = 'app.egress.override' THEN 'app.egress.loosen' ELSE v END), '[]'::jsonb)
         FROM jsonb_array_elements_text(body->'agent_disabled_verbs') AS v))
WHERE body->'agent_disabled_verbs' ? 'app.egress.override';

-- The egress rules a deployment ran with, merged from the install's and the
-- app's. The reconciler restores these, so a running app is not changed
-- underneath it by a policy edit (R-183, O-10). NULL for a deployment from
-- before egress was enforced, which ran unrestricted.
ALTER TABLE deployments ADD COLUMN egress_rules jsonb;

-- Deploy approval (R-154 – R-159). A deploy that needs approval is a
-- deployment row that waits: tied to one spec revision (R-156), listed with
-- the app's other deploys, and moved to pending — the ordinary path — when it
-- has its approvals.
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('awaiting_approval', 'pending', 'building', 'applying',
                      'succeeded', 'failed', 'superseded', 'rejected', 'expired'));

-- How many approvals it needs, and when it stops waiting. Fixed when the
-- request is made, so a policy edit does not move a request's goalposts.
-- NULL expires_at is a request that waits until answered.
ALTER TABLE deployments ADD COLUMN approvals_required integer;
ALTER TABLE deployments ADD COLUMN approval_expires_at timestamptz;
-- Why it needed approval: install, app_policy, app_spec, egress_loosening.
ALTER TABLE deployments ADD COLUMN approval_reasons text[];

CREATE INDEX deployments_awaiting_idx ON deployments (approval_expires_at)
    WHERE status = 'awaiting_approval';

-- One row per decision. A principal decides once per request: approving twice
-- does not count twice.
CREATE TABLE deployment_approvals (
    deployment_id text NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    principal_id  text NOT NULL,
    decision      text NOT NULL CHECK (decision IN ('approve', 'reject')),
    comment       text,
    decided_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (deployment_id, principal_id)
);
