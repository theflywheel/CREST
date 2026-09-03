-- The funders wave (F-1, F-2): rate ownership and the payment mechanism.
--
-- The layering test drew every line here. Rate amounts, currencies, batching
-- windows, file formats and cadences are L2 values and live in opaque columns.
-- What is L1 — what no two deployments could reasonably disagree about — is
-- that these ACTS are recorded: who was assigned to own rates and by whom
-- (f1_2), who chose the batching timing and when (f2_7), whether the whole
-- payment path was proven once and with what result (f2_4), and whether the
-- mechanism is live (f2_8). The rate itself is NOT here: a rate is a
-- payment-setup LinkedRecord on the Definition (§10, §7), versioned and never
-- edited; this schema holds only the application records around it.

-- f1_2: who may author rates for a definition. Assignment history is kept —
-- a superseded assignment is evidence of who owned the rate when it was set,
-- which matters exactly when a rate is questioned later.
CREATE TABLE rate_owner_assignments (
    id                   text PRIMARY KEY,
    definition_id        text NOT NULL,
    assignee_party_id    text NOT NULL,
    assigned_by_party_id text NOT NULL,
    assigned_at          timestamptz NOT NULL,
    superseded_at        timestamptz
);
-- One current owner per definition: "Anyone can ask. Only one person can
-- assign" — and only one person holds it at a time.
CREATE UNIQUE INDEX rate_owner_current ON rate_owner_assignments (definition_id)
    WHERE superseded_at IS NULL;

-- f2_8: the mechanism record. One per context; CONFIGURED is a real state a
-- deployment can sit in (f1_5: "half done is a real state"), and ACTIVE is
-- reached only through the activation gate, never by insert.
CREATE TABLE mechanisms (
    id                  text PRIMARY KEY,
    context_id          text NOT NULL UNIQUE,
    owner_party_id      text NOT NULL,
    state               text NOT NULL CHECK (state IN ('CONFIGURED', 'ACTIVE')),
    -- Rail choice, account references, cadences: all L2, all opaque here.
    config              jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by_party_id text NOT NULL,
    created_at          timestamptz NOT NULL,
    activated_at        timestamptz,
    activated_by        text,
    -- Activation is an act with an actor, or it has not happened.
    CONSTRAINT activation_is_an_act CHECK (
        state <> 'ACTIVE' OR (activated_at IS NOT NULL AND activated_by IS NOT NULL)
    )
);

-- f2_5, f2_6, f2_7, f2_9, f2_10: the recorded acts of setting a mechanism up.
-- kind names the act; payload carries the L2 values (format, cadence, window,
-- trade-off text) opaquely; actor and time are the L1 facts.
CREATE TABLE mechanism_records (
    id             text PRIMARY KEY,
    mechanism_id   text NOT NULL REFERENCES mechanisms(id),
    kind           text NOT NULL,
    actor_party_id text NOT NULL,
    payload        jsonb NOT NULL DEFAULT '{}'::jsonb,
    at             timestamptz NOT NULL
);
CREATE INDEX mechanism_records_by_mechanism ON mechanism_records (mechanism_id, kind, at);

-- f2_4: one test disbursement through the configured mechanism, recorded with
-- its result. FAILED rows keep their failure with an owner: an unexplained
-- failed test is the same shape of silence as an unexplained held payment.
CREATE TABLE test_disbursements (
    id                 text PRIMARY KEY,
    mechanism_id       text NOT NULL REFERENCES mechanisms(id),
    requested_by       text NOT NULL,
    amount_minor       bigint NOT NULL,
    currency           text NOT NULL,
    destination        text NOT NULL,
    state              text NOT NULL CHECK (state IN ('SUCCEEDED', 'FAILED')),
    rail_ref           text,
    failure_code       text,
    failure_reason     text,
    failure_owner      text,
    at                 timestamptz NOT NULL,
    CONSTRAINT failed_tests_have_a_reason_and_an_owner CHECK (
        state <> 'FAILED' OR (failure_code IS NOT NULL AND failure_reason IS NOT NULL
                              AND failure_owner IS NOT NULL)
    )
);

-- f2_9: the gate sits in front of DISBURSEMENT, not in front of the window
-- exit. The exit still creates the instruction; when the mechanism is not
-- live it is created HELD with reason mechanism_not_live and the mechanism
-- owner named — which is why the instruction now remembers its context, so
-- activation can find and release exactly the payments it was holding.
ALTER TABLE instructions ADD COLUMN context_id text;
CREATE INDEX instructions_held_by_context ON instructions (context_id)
    WHERE state = 'HELD';
