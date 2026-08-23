-- notify: reaching a worker (§9, W2).
--
-- The table exists so that "was this worker told?" has an answer. W2 says a
-- worker sees what was recorded about them before it counts; a notification
-- that was attempted and lost, with no row anywhere, makes that unfalsifiable.

CREATE TABLE notifications (
    id          text PRIMARY KEY,
    party_id    text NOT NULL,
    claim_id    text,
    kind        text NOT NULL,
    channel     text NOT NULL,
    destination text NOT NULL,
    body        text NOT NULL,
    state       text NOT NULL CHECK (state IN ('SENT', 'FAILED', 'UNREACHABLE')),
    failure     text,
    created_at  timestamptz NOT NULL,

    -- One notification per claim per kind. The window opens once, and telling
    -- a worker the same thing four times because a relay retried is its own
    -- kind of harm.
    UNIQUE (party_id, claim_id, kind)
);
CREATE INDEX notifications_by_party ON notifications (party_id, created_at);
