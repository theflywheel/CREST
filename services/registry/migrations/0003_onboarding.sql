-- Organisation self-registration, terms acceptance, approval, and assisted
-- enrolment (#20, Blueprint §3).
--
-- Two onboarding paths, and they are not symmetrical. An organisation applies
-- for itself and someone decides. A worker is often enrolled by someone else
-- entirely — a field worker with a phone standing next to a worker without one
-- — and W1 says a worker must be able to exist without a document, a phone or
-- literacy. A design that only supports self-service excludes exactly the
-- people this system is for.

-- An organisation's application, separate from its Party.
--
-- The Party is the thing that exists; this is the thing that was decided about
-- it. Keeping them apart means a rejected application leaves a record rather
-- than a deleted row, and an approval can be revoked without destroying the
-- organisation's identity or the work already recorded against it.
CREATE TABLE org_registrations (
    party_id        text PRIMARY KEY REFERENCES parties(id) ON DELETE CASCADE,
    state           text NOT NULL CHECK (state IN ('APPLIED', 'TERMS_ACCEPTED', 'APPROVED', 'REJECTED')),
    terms_id        text,
    terms_version   integer,
    -- Who accepted, and when. An acceptance with no acceptor is an acceptance
    -- nobody can be asked about.
    accepted_by     text,
    accepted_at     timestamptz,
    -- Author of the application may not be the approver, enforced below. The
    -- same separation §7 requires of a definition, for the same reason: an
    -- approval you can grant yourself is not an approval.
    decided_by      text,
    decided_at      timestamptz,
    reason          text,
    applied_at      timestamptz NOT NULL,

    CONSTRAINT approval_is_not_self_granted
        CHECK (decided_by IS NULL OR decided_by <> party_id),
    -- A decision needs a decider and a time. A state that says APPROVED with
    -- nobody attached is the kind of row that reads as authorised forever.
    CONSTRAINT a_decision_has_an_owner
        CHECK ((state IN ('APPROVED', 'REJECTED')) = (decided_by IS NOT NULL AND decided_at IS NOT NULL)),
    -- Terms cannot be half-accepted.
    CONSTRAINT terms_acceptance_is_complete
        CHECK ((terms_id IS NULL) = (terms_version IS NULL))
);
CREATE INDEX org_registrations_pending ON org_registrations (state) WHERE state <> 'APPROVED';

-- Assisted enrolment: who enrolled whom.
--
-- Recorded rather than inferred, because it is provenance the assurance
-- derivation reads and a supervisor may later be asked about. It is deliberately
-- NOT a claim that the enroller vouches for the worker's identity — that is what
-- the Party's identityBindings say, and the assurance level stays derived from
-- those. An enrolment row raises no assurance by itself, which is the whole
-- point: a field worker's say-so is a provenance fact, not an identity proof.
CREATE TABLE assisted_enrolments (
    party_id        text PRIMARY KEY REFERENCES parties(id) ON DELETE CASCADE,
    enrolled_by     text NOT NULL,
    context_id      text,
    method          text NOT NULL CHECK (method IN ('supervisor-attested', 'roster-import', 'field-visit')),
    note            text,
    enrolled_at     timestamptz NOT NULL,

    -- Nobody enrols themselves through this path; that is ordinary
    -- registration, and calling it assisted would overstate what was witnessed.
    CONSTRAINT enrolment_is_by_someone_else CHECK (enrolled_by <> party_id)
);
CREATE INDEX assisted_enrolments_by_enroller ON assisted_enrolments (enrolled_by);
