-- Presentation requests: §9's disclosure consent made a record (w1_15, w1_19,
-- w1_20).
--
-- "Who may see what, per presentation. Showing the bare QR is itself consent
-- for the non-identifying payload; anything more requires an explicit, scoped,
-- recorded grant naming who asked and why. Enforced by the verification
-- service, recorded in the private consent store." This table is that grant's
-- lifecycle: a verifier asks, naming themselves and a purpose the worker will
-- actually read; the worker answers per share, every time; the list the worker
-- approved is exactly the list the verifier collects.
--
-- Nothing in a row is a raw identifier: party ids and credential ids only.
CREATE TABLE presentation_requests (
    id            text PRIMARY KEY,
    subject_party_id text NOT NULL,
    requested_by  text NOT NULL,
    -- Free text, 10–200 characters, same ruling as the batch purpose (§9,
    -- 2026-08-25): it ends up in front of the worker, who should not need a
    -- codebook.
    purpose       text NOT NULL CHECK (char_length(purpose) BETWEEN 10 AND 200),
    -- NULL means the verifier asked about the whole chain; the worker still
    -- sees the resolved list before deciding, and approves ids explicitly.
    requested_credential_ids jsonb,
    state         text NOT NULL CHECK (state IN ('REQUESTED', 'APPROVED', 'DECLINED', 'FULFILLED')),
    approved_credential_ids  jsonb,
    decline_reason text,
    created_at    timestamptz NOT NULL,
    -- A request does not stand open forever: past this, deciding and
    -- collecting are both refused. EXPIRED is derived at read, never stored —
    -- the same rule as every other judgement here.
    expires_at    timestamptz NOT NULL,
    decided_at    timestamptz,
    fulfilled_at  timestamptz,
    -- An approval with no list, or a refusal with no reason, must be
    -- impossible to express, not discouraged.
    CONSTRAINT approvals_carry_their_list CHECK (
        state NOT IN ('APPROVED', 'FULFILLED') OR approved_credential_ids IS NOT NULL),
    CONSTRAINT refusals_carry_a_reason CHECK (
        state <> 'DECLINED' OR (decline_reason IS NOT NULL AND decline_reason <> ''))
);
CREATE INDEX presentation_requests_by_subject ON presentation_requests (subject_party_id, created_at);
CREATE INDEX presentation_requests_by_verifier ON presentation_requests (requested_by, created_at);
