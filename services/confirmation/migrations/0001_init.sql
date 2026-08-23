-- confirmation: is it true? The T=7 window, and issuance (§9, §13).
--
-- The table this service exists for is `windows`. Every claim gets one, every
-- window has exactly one exit, and all four exits release payment (W4). The
-- shape below is what makes that checkable rather than asserted: a window with
-- no exit past its closing time is a query, and a released_payment flag that is
-- false on a closed window is a bug you can alert on.

CREATE TABLE windows (
    claim_id            text PRIMARY KEY,
    unit_id             text NOT NULL,
    party_id            text NOT NULL,
    context_id          text NOT NULL,
    definition_id       text NOT NULL,
    definition_version  integer NOT NULL,
    opened_at           timestamptz NOT NULL,
    closes_at           timestamptz NOT NULL,
    notified_at         timestamptz,

    -- Which of the four exits was taken, and when. NULL means still open.
    exit_route          text CHECK (exit_route IN ('self', 'auto', 'assisted', 'dispute')),
    exited_at           timestamptz,

    -- Recorded separately from the exit so that "exited but never released" is
    -- a state the database can show you. If these were one column, a payment
    -- that failed to release would be indistinguishable from one that did.
    payment_released_at timestamptz,
    credential_id       text
);
CREATE INDEX windows_open ON windows (closes_at) WHERE exit_route IS NULL;

-- What CREST keeps about a credential: enough to find it, revoke it and tie a
-- presented one back to the claim. Not the credential — the wallet holds the
-- only complete copy, and there is deliberately no register (§3).
CREATE TABLE credentials (
    id            text PRIMARY KEY,
    claim_id      text NOT NULL UNIQUE,
    subject_ref   text NOT NULL,
    status_index  integer NOT NULL UNIQUE,
    digest        text NOT NULL,
    doc           jsonb NOT NULL,
    issued_at     timestamptz NOT NULL,
    revoked_at    timestamptz
);

-- The revocation bitstring, as one row. It is bulk data by design: a verifier
-- fetches the whole list, so checking one credential reveals nothing about
-- which one was checked (§9).
CREATE TABLE status_list (
    id      integer PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    bits    bytea NOT NULL,
    next_index integer NOT NULL DEFAULT 0
);
INSERT INTO status_list (id, bits) VALUES (1, ''::bytea);

CREATE TABLE contests (
    id            text PRIMARY KEY,
    target_kind   text NOT NULL,
    target_id     text NOT NULL,
    raised_by     text NOT NULL,
    reason        text NOT NULL,
    state         text NOT NULL,
    doc           jsonb NOT NULL,
    created_at    timestamptz NOT NULL
);
CREATE INDEX contests_by_target ON contests (target_kind, target_id);

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
