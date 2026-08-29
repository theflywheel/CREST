-- evidence: what happened (§13). Adapters in, units and claims out.
--
-- The unit/claim split is the load-bearing shape of the whole system (§2): a
-- Unit exists independent of who performed it, and a Claim links a Party to it.
-- They are separate tables with no cascade from claim to unit, because a
-- disputed claim must never destroy the underlying record (W5).

CREATE TABLE batches (
    id            text PRIMARY KEY,
    context_id    text NOT NULL,
    definition_id text NOT NULL,
    definition_version integer NOT NULL,
    submitted_by  text NOT NULL,
    adapter_ref   text NOT NULL,
    rows_total    integer NOT NULL,
    rows_accepted integer NOT NULL,
    rows_unclear  integer NOT NULL,
    created_at    timestamptz NOT NULL
);

CREATE TABLE units (
    id          text PRIMARY KEY,
    batch_id    text REFERENCES batches(id),
    context_id  text NOT NULL,
    definition_id text NOT NULL,
    definition_version integer NOT NULL,
    doc         jsonb NOT NULL,
    created_at  timestamptz NOT NULL
);
CREATE INDEX units_by_context ON units (context_id, created_at);

CREATE TABLE claims (
    id          text PRIMARY KEY,
    -- No ON DELETE CASCADE, and units are never deleted. The reference points
    -- from claim to unit and not the other way for the same reason: the record
    -- that work happened outlives every argument about who did it.
    unit_id     text NOT NULL REFERENCES units(id),
    party_id    text NOT NULL,
    state       text NOT NULL CHECK (state IN ('DRAFT', 'NOTIFIED', 'ACCEPTED', 'DISPUTED')),
    doc         jsonb NOT NULL,
    created_at  timestamptz NOT NULL,

    -- One claim per party per unit. Re-running a batch is an ordinary
    -- operational event, and it must not pay anyone twice.
    UNIQUE (unit_id, party_id)
);
CREATE INDEX claims_by_party ON claims (party_id, state);

-- The unclear queue. Rows that describe work nobody could attribute.
--
-- They are kept, not discarded: the work happened, and the row is the only
-- evidence of it. A queue nobody can list is a queue nobody works, so this is a
-- table with a reason column rather than a log line.
CREATE TABLE unclear_rows (
    id           text PRIMARY KEY,
    batch_id     text NOT NULL REFERENCES batches(id),
    row_ref      text NOT NULL,
    reason       text NOT NULL,
    -- The record as parsed, so the row can be re-attributed once the person is
    -- identified rather than asked for again.
    record       jsonb,
    created_at   timestamptz NOT NULL,
    resolved_at  timestamptz,
    resolved_to  text
);
CREATE INDEX unclear_open ON unclear_rows (resolved_at) WHERE resolved_at IS NULL;

-- The transactional outbox: a claim's creation and the message that opens its
-- confirmation window commit together, or neither does (W2, W4).
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
