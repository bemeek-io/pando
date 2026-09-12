-- Idempotency keys for infrastructure-creating requests (design 04, phase 10).
--
-- Required for MCP: an agent retries on a timeout, and without this a retried
-- deploy deploys twice. The retry is not a hypothetical — a deploy resolves a
-- ref, which means cloning, which regularly outlasts a client's patience.
--
-- The key is scoped to the principal as well as the endpoint. Two agents
-- choosing the same key is not far-fetched (models pick round numbers), and
-- without the principal in the key one agent's retry would return the other's
-- result — which is worse than deploying twice, because it looks like success.
CREATE TABLE idempotency_keys (
    key          text NOT NULL,
    principal_id text NOT NULL,
    endpoint     text NOT NULL,

    -- The response to replay. Stored rather than recomputed: the point is that
    -- the second call has no effect, and recomputing is an effect.
    status_code  integer NOT NULL,
    body         jsonb   NOT NULL,

    created_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (key, principal_id, endpoint)
);

-- Keys are swept after a day. Long enough that any honest retry is covered —
-- retries happen in seconds or minutes — and short enough that the table does
-- not grow forever. A key reused after the sweep is a new request, which is
-- correct: nobody retries something from yesterday.
CREATE INDEX idempotency_keys_swept_idx ON idempotency_keys (created_at);
