-- Durable request fingerprints for mutations. Each service that uses the
-- package applies this additive migration in its own schema. The table stores
-- no request or response body, only the actor, route, digest and a resource
-- reference from which a private replay can be reconstructed.
CREATE TABLE idempotency_requests (
    request_key text NOT NULL,
    actor_id text NOT NULL,
    method text NOT NULL,
    path text NOT NULL,
    body_digest text NOT NULL,
    state text NOT NULL CHECK (state IN ('in_progress', 'completed')),
    status integer,
    resource_type text,
    resource_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    PRIMARY KEY (actor_id, request_key),
    CHECK (
        (state = 'in_progress' AND status IS NULL AND completed_at IS NULL)
        OR
        (state = 'completed' AND status IS NOT NULL AND completed_at IS NOT NULL)
    ),
    CHECK ((resource_type IS NULL) = (resource_id IS NULL))
);

CREATE INDEX idempotency_requests_created_at
    ON idempotency_requests (created_at);
