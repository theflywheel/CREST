-- G-2's missing backend: project→org invitations and terms-upgrade requests
-- (Blueprint §15 J1, screens g2_5–g2_13).
--
-- Same shape as 0012: the document is the record, the columns are an index,
-- and every state machine gets an append-only trail with an actor on every
-- row. No table below stores a document taxonomy, a check catalogue or a
-- terms vocabulary — those are L2, and they live as opaque strings inside the
-- documents and trail rows.
--
-- Never persist a raw national ID, biometric — or, here, a raw document at
-- all. A qualification document is declared as {kind, ref, hash}: a reference
-- into the deployment's own document custody, bounded and single-line. This
-- service has no blob store and this migration adds none.

-- The offer whose acceptance creates a partner grant. Not a new primitive:
-- the standing fact is the Authorization the acceptance writes, and this
-- table is the recorded offer-and-answer that produced it.
CREATE TABLE project_invitations (
    id          text PRIMARY KEY,
    context_id  text NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
    party_id    text NOT NULL REFERENCES parties(id),
    doc         jsonb NOT NULL,
    state       text NOT NULL CHECK (state IN ('SENT', 'ACCEPTED', 'DECLINED')),
    created_at  timestamptz NOT NULL
);
CREATE INDEX invitations_by_party   ON project_invitations (party_id, state);
CREATE INDEX invitations_by_context ON project_invitations (context_id, state);

-- The invitation's trail. QUESTION is g2_9's "Ask a question" — conversation
-- on the record, either side, only while the offer is open. Append-only: a
-- declined invitation's reason is exactly the fact the inviting project needs
-- after the decline stops being current.
CREATE TABLE invitation_events (
    invitation_id  text NOT NULL REFERENCES project_invitations(id) ON DELETE CASCADE,
    seq            integer NOT NULL,
    event          text NOT NULL CHECK (event IN ('SENT', 'QUESTION', 'ACCEPTED', 'DECLINED')),
    actor_party_id text NOT NULL,
    note           text,
    at             timestamptz NOT NULL,
    PRIMARY KEY (invitation_id, seq)
);

-- An organisation's request to move to a wider published terms version
-- (g2_6–g2_8). DRAFT → SUBMITTED → APPROVED | DENIED | WITHDRAWN; a settled
-- request is never reopened — asking again is a new row, and the old answer
-- survives as its own record.
CREATE TABLE terms_requests (
    id          text PRIMARY KEY,
    party_id    text NOT NULL REFERENCES parties(id),
    doc         jsonb NOT NULL,
    state       text NOT NULL CHECK (state IN ('DRAFT', 'SUBMITTED', 'WITHDRAWN', 'APPROVED', 'DENIED')),
    created_at  timestamptz NOT NULL
);
CREATE INDEX terms_requests_by_party ON terms_requests (party_id, state);
CREATE INDEX terms_requests_by_state ON terms_requests (state);

CREATE TABLE terms_request_events (
    request_id     text NOT NULL REFERENCES terms_requests(id) ON DELETE CASCADE,
    seq            integer NOT NULL,
    event          text NOT NULL CHECK (event IN ('DRAFTED', 'SUBMITTED', 'WITHDRAWN', 'APPROVED', 'DENIED')),
    actor_party_id text NOT NULL,
    reason         text,
    at             timestamptz NOT NULL,
    PRIMARY KEY (request_id, seq)
);

-- Check verdicts on a submitted request (g2_12, "what is checked before this
-- is live"). The check NAMES are the deployment's catalogue — opaque here.
-- What is infrastructure is that every verdict has a binary outcome and an
-- owner: a party who ran it, or a named policy (the eventual business-register
-- adapter's slot). No automated checker exists in this codebase today, and
-- this table is deliberately a record of verdicts, not a fake of automation.
CREATE TABLE terms_request_checks (
    request_id  text NOT NULL REFERENCES terms_requests(id) ON DELETE CASCADE,
    seq         integer NOT NULL,
    name        text NOT NULL,
    outcome     text NOT NULL CHECK (outcome IN ('PASS', 'FAIL')),
    owner_kind  text NOT NULL CHECK (owner_kind IN ('party', 'policy')),
    owner       text NOT NULL,
    note        text,
    recorded_by text NOT NULL,
    at          timestamptz NOT NULL,
    PRIMARY KEY (request_id, seq)
);
