-- Enrolment consent (§9, #24).
--
-- The first of §9's three consent moments, and the one that decides whether a
-- worker who cannot read can take part at all. §9: "the right to fetch and hold
-- evidence about the worker. Recorded once, revocable; voice recording is a
-- valid capture for non-literate workers."
--
-- Two things this table is shaped by.
--
-- Revocable means revocable in effect, not in a column. A withdrawal that
-- changes nothing is a checkbox, so `resolve` reports the state and evidence
-- refuses new records for a worker who has withdrawn. Work already recorded is
-- untouched: consent governs what may be collected next, and taking away the
-- record of work somebody did would punish them for withdrawing.
--
-- The artefact never lives here. It is in the object store, and this row holds
-- the key and the digest — the digest so that an artefact swapped underneath us
-- is detectable rather than trusted, which matters precisely because this is
-- the evidence that a person agreed to something.
CREATE TABLE consents (
    id              text PRIMARY KEY,
    party_id        text NOT NULL REFERENCES parties(id) ON DELETE CASCADE,

    -- Only 'enrolment' today. The column exists because §9 is emphatic that the
    -- three moments are distinct objects and never collapsed, and a table that
    -- cannot tell them apart is how they get collapsed.
    moment          text NOT NULL CHECK (moment IN ('enrolment', 'issuance-confirmation', 'disclosure')),

    purpose         text NOT NULL,
    capture_method  text NOT NULL CHECK (capture_method IN ('screen', 'sms', 'ussd', 'voice', 'assisted')),

    -- Who took the consent, when it is assisted. A consent captured by somebody
    -- else with nobody named is a consent nobody can be asked about.
    captured_by     text,
    captured_at     timestamptz NOT NULL,

    artefact_key    text,
    artefact_digest text,
    artefact_type   text,

    revoked_at      timestamptz,
    revoked_reason  text,

    -- A voice consent with no recording is not a voice consent. It is a claim
    -- that somebody spoke, with nothing to show, and for a worker who cannot
    -- read the form it would be the whole of their consent record.
    --
    -- "While it stands" is the necessary qualifier, and writing it without that
    -- made withdrawal impossible: withdrawing deletes the recording, which the
    -- unqualified constraint then rejected. The two rules are not in tension
    -- once stated properly — a live voice consent must have its recording, and
    -- a withdrawn one must eventually not, because deleting it is what makes
    -- the withdrawal real.
    CONSTRAINT a_live_voice_consent_has_its_recording
        CHECK (capture_method <> 'voice' OR artefact_key IS NOT NULL OR revoked_at IS NOT NULL),

    -- An assisted capture names its agent, for the same reason an assisted
    -- enrolment does.
    CONSTRAINT assisted_consent_names_who_took_it
        CHECK (capture_method NOT IN ('voice', 'assisted') OR captured_by IS NOT NULL),

    -- The key and the digest travel together. A key with no digest is an
    -- artefact nobody can prove was not replaced.
    CONSTRAINT artefact_is_complete
        CHECK ((artefact_key IS NULL) = (artefact_digest IS NULL))
);

-- One live enrolment consent per party: §9 says "recorded once". A withdrawn
-- one does not block a fresh grant, because a worker who changes their mind
-- twice is exercising the same right twice.
CREATE UNIQUE INDEX consents_one_live_enrolment
    ON consents (party_id) WHERE moment = 'enrolment' AND revoked_at IS NULL;

CREATE INDEX consents_by_party ON consents (party_id, moment);
