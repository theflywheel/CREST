-- Contest decisions and corrections are append-only events. The original
-- signed claim and credential remain immutable; a CORRECTED decision points to
-- a separate correction event for the attestation service to process.
CREATE TABLE contest_decisions (
    id             text PRIMARY KEY,
    contest_id     text NOT NULL REFERENCES contests(id),
    decision       text NOT NULL CHECK (decision IN ('UPHELD', 'CORRECTED', 'REJECTED')),
    decided_by     text NOT NULL,
    reason         text NOT NULL,
    evidence       text NOT NULL,
    decided_at     timestamptz NOT NULL,
    doc            jsonb NOT NULL
);
CREATE INDEX contest_decisions_by_contest ON contest_decisions (contest_id, decided_at, id);

CREATE TABLE correction_events (
    id                text PRIMARY KEY,
    contest_id        text NOT NULL REFERENCES contests(id),
    decision_id       text NOT NULL REFERENCES contest_decisions(id),
    claim_id          text NOT NULL,
    credential_id     text,
    replacement_ref   text,
    reason            text NOT NULL,
    evidence          text NOT NULL,
    emitted_at        timestamptz NOT NULL,
    doc               jsonb NOT NULL
);
CREATE INDEX correction_events_by_claim ON correction_events (claim_id, emitted_at, id);
