-- Issuance moves into the credential substrate (#137), and its tables come
-- with it. These are the same shapes the confirmation service created in its
-- 0001_init.sql, which now sit unused on any stack that predates this.

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

-- A deployment that already issued from the confirmation service keeps every
-- credential and every revocation: the old tables are copied, not abandoned.
-- Copy rather than move, so a not-yet-upgraded confirmation binary starting
-- later finds its schema intact; its tables are dropped in a later cleanup
-- once the copy is verified on every long-lived stack.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = 'confirmation' AND table_name = 'credentials') THEN
        INSERT INTO credentials (id, claim_id, subject_ref, status_index, digest, doc, issued_at, revoked_at)
        SELECT id, claim_id, subject_ref, status_index, digest, doc, issued_at, revoked_at
          FROM confirmation.credentials
        ON CONFLICT (id) DO NOTHING;
        UPDATE status_list v
           SET bits = c.bits, next_index = c.next_index
          FROM confirmation.status_list c
         WHERE v.id = 1 AND c.id = 1;
    END IF;
END $$;
