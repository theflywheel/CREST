-- The payments application becomes one service (#129): the confirmation
-- window's tables join the instruction's, behind one schema boundary. The
-- shapes are the ones confirmation's own migrations built, reach columns
-- included; a stack that already ran them keeps every window and contest —
-- copied from the confirmation schema below, not abandoned in it.

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
    credential_id       text,

    -- Auto-confirmation must not be applied to a worker nobody reached: a
    -- window marked unreached is surfaced, never swept.
    reach               text CHECK (reach IS NULL OR reach IN ('reached', 'unreached')),
    reach_detail        text,
    escalated_at        timestamptz
);
CREATE INDEX windows_open ON windows (closes_at) WHERE exit_route IS NULL;
CREATE INDEX windows_unreached ON windows (reach) WHERE exit_route IS NULL;

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

-- A deployment that ran the two-service shape keeps its history. Undelivered
-- outbox rows are deliberately not copied: the old confirmation service is
-- retired only after its outbox drains, which the runbook states.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = 'confirmation' AND table_name = 'windows') THEN
        INSERT INTO windows
        SELECT * FROM confirmation.windows
        ON CONFLICT (claim_id) DO NOTHING;
        INSERT INTO contests
        SELECT * FROM confirmation.contests
        ON CONFLICT (id) DO NOTHING;
    END IF;
END $$;
