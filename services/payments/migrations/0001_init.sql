-- payments: what should be paid, what was, and where is the difference (§10, §13).
--
-- Instructions and compensations are LinkedRecords keyed to a Unit by event id
-- (§2.1). They are never inside the credential: the credential path completes
-- whether or not money moves, and the money moves whether or not a credential
-- was issued.

CREATE TABLE instructions (
    id            text PRIMARY KEY,
    -- One instruction per claim, enforced here rather than in a handler. The
    -- release message is delivered at-least-once, and a unique index is the
    -- only thing that makes a redelivery harmless.
    claim_id      text NOT NULL UNIQUE,
    unit_id       text NOT NULL,
    party_id      text NOT NULL,
    amount_minor  bigint NOT NULL,
    currency      text NOT NULL,
    released_by   text NOT NULL CHECK (released_by IN ('self', 'auto', 'assisted', 'dispute')),
    released_at   timestamptz NOT NULL,
    state         text NOT NULL CHECK (state IN ('RELEASED', 'HELD')),

    -- W10: every held payment has a reason with an owner. Not nullable
    -- independently — a held instruction with no owner is the failure mode this
    -- constraint exists to make impossible.
    held_code       text,
    held_reason     text,
    held_owner      text,
    CONSTRAINT held_payments_have_a_reason_and_an_owner CHECK (
        state <> 'HELD' OR (held_code IS NOT NULL AND held_reason IS NOT NULL AND held_owner IS NOT NULL)
    ),

    doc           jsonb NOT NULL,
    created_at    timestamptz NOT NULL
);
CREATE INDEX instructions_held ON instructions (state) WHERE state = 'HELD';

CREATE TABLE compensations (
    id              text PRIMARY KEY,
    instruction_id  text NOT NULL REFERENCES instructions(id),
    unit_id         text NOT NULL,
    amount_minor    bigint NOT NULL,
    currency        text NOT NULL,
    rail_ref        text,
    state           text NOT NULL CHECK (state IN ('SENT', 'CONFIRMED', 'FAILED')),
    failure_code    text,
    failure_reason  text,
    failure_owner   text,
    confirmed_at    timestamptz,
    doc             jsonb NOT NULL,
    created_at      timestamptz NOT NULL,
    UNIQUE (instruction_id)
);

CREATE TABLE outbox (
    id              bigserial PRIMARY KEY,
    topic           text NOT NULL,
    payload         jsonb NOT NULL,
    attempts        integer NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    claimed_at      timestamptz,
    delivered_at    timestamptz,
    next_attempt_at timestamptz,
    last_error      text
);
CREATE INDEX outbox_pending ON outbox (id) WHERE delivered_at IS NULL;
