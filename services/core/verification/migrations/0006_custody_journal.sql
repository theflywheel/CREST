-- Explicit wallet custody handoff. The signed document is deleted only after
-- the worker has acknowledged durable encrypted storage. Claim, digest,
-- status, and audit metadata remain for idempotency and revocation.
ALTER TABLE credentials ALTER COLUMN doc DROP NOT NULL;

CREATE TABLE custody_journal (
    credential_id text PRIMARY KEY REFERENCES credentials(id),
    subject_ref text NOT NULL,
    expected_digest text NOT NULL,
    storage_kind text NOT NULL CHECK (storage_kind = 'encrypted-wallet'),
    transferred_at timestamptz NOT NULL
);
