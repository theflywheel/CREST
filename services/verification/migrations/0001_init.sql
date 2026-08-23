-- verification: does it stand, how strongly, on whose authority? (§7, §13)

-- Disclosure consent (§9). Showing the bare QR is itself consent for the
-- non-identifying payload; anything more is explicit, scoped and recorded —
-- naming who asked and why. This table is that record.
CREATE TABLE presentations (
    id            text PRIMARY KEY,
    credential_id text,
    subject_ref   text,
    requested_by  text,
    purpose       text,
    scope         text NOT NULL CHECK (scope IN ('bare', 'scoped')),
    outcome       text NOT NULL,
    tier          integer,
    created_at    timestamptz NOT NULL
);
CREATE INDEX presentations_by_subject ON presentations (subject_ref, created_at);

-- How this deployment currently regards a source (§6).
--
-- This is the mechanism behind "re-assessing a compromised source system
-- downgrades every affected credential's tier instantly, without reissuance".
-- The credential does not change; the answer to what it is worth does. That is
-- only possible because the tier was never stored on it.
CREATE TABLE source_assessments (
    adapter_ref  text PRIMARY KEY,
    max_tier     integer NOT NULL CHECK (max_tier >= 0 AND max_tier <= 3),
    reason       text NOT NULL,
    assessed_by  text NOT NULL,
    assessed_at  timestamptz NOT NULL
);
