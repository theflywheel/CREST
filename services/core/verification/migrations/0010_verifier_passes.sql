-- The verifier pass (#27, G1 #9; Blueprint J9 L1: "pass issuance — name +
-- reachable contact, no account, no vetting").
--
-- A pass identifies a stranger who checks credentials without onboarding
-- them. It exists so that every online check is recorded against somebody the
-- worker can see (W8) and so the per-pass rate cap G1 ruled on has something to
-- be per. It grants nothing else: no accreditation ceiling, no batch rights.
--
-- The contact is the pass's identity — one pass per contact, and asking again
-- rotates the token rather than minting a second pass — because a cap per pass
-- would otherwise be a cap per request for a new pass. Only the hash of the
-- token is kept; the token is shown once, at issuance.
CREATE TABLE verifier_passes (
    id          text PRIMARY KEY,
    name        text NOT NULL,
    contact     text NOT NULL UNIQUE,
    token_hash  text NOT NULL UNIQUE,
    issued_at   timestamptz NOT NULL,
    rotated_at  timestamptz,
    revoked_at  timestamptz
);

-- A check made against a pass is its own scope in the trail: the worker sees
-- a named stranger, not an onboarded party and not an anonymous scan.
ALTER TABLE presentations DROP CONSTRAINT presentations_scope_check;
ALTER TABLE presentations
    ADD CONSTRAINT presentations_scope_check
    CHECK (scope IN ('bare', 'scoped', 'consented', 'pass'));

-- The rate cap is counted from the trail itself — the rows a requester left
-- in the window — so the cap is auditable from the same record the worker
-- reads, and survives a restart or a second replica.
CREATE INDEX presentations_by_requester ON presentations (requested_by, created_at);
