-- Party invitations: the narrowing of finding #123.
--
-- An UNBOUND party id has always been a bootstrap capability — whoever learns
-- the id of a never-bound party can claim it with their own token. That was
-- accepted because ids are unguessable ULIDs and enrolment binds promptly.
-- It is also the only way a person who is not a worker ever reaches a
-- console: somebody creates their record, and they bind to it on first login.
--
-- This table makes that step a recorded, single-use, expiring act with an
-- inviter's name on it instead of an unstated property of ids. A code is
-- shown once to the inviter and travels out of band; only its hash is kept.
-- Claiming it appends the claimant's own identity binding to the party — the
-- same append-only history as every other binding — and the row records
-- who claimed and when. Never a raw national identifier, never the code.
CREATE TABLE party_invitations (
    code_hash       text PRIMARY KEY,
    party_id        text NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    invited_by      text NOT NULL,
    created_at      timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    claimed_at      timestamptz,
    -- The deployment's pairwise reference for whoever claimed it: the same
    -- value the binding carries, kept here so the invitation row alone can
    -- answer "who took this".
    claimed_subject text
);
CREATE INDEX party_invitations_by_party ON party_invitations (party_id, claimed_at);
