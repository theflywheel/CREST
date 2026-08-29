-- Recovery: a worker who lost their device gets their record back (§16, #106).
--
-- Two paths, one destination. 2-of-3: two people from DIFFERENT authorities
-- confirm this is the same person. Override: where two confirmers cannot be
-- reached, a supervisor holding `override-recovery` decides alone — and the
-- constraint below is the whole safeguard: an override is not hard to obtain,
-- it is impossible to make quietly. Same shape as a held payment: the rule is
-- not that overrides are rare but that every one has a reason and a name.
CREATE TABLE recoveries (
    id            text NOT NULL PRIMARY KEY,
    party_id      text NOT NULL REFERENCES parties(id),
    opened_by     text NOT NULL,
    reason        text NOT NULL,
    state         text NOT NULL CHECK (state IN ('OPEN', 'CONFIRMED', 'OVERRIDDEN', 'COMPLETED')),
    created_at    timestamptz NOT NULL,

    -- The override record. Permanent — recovery rows are never deleted — and
    -- refused by the database itself unless it carries an owner, a reason and
    -- a review-by date. "Flagged for review, never silent": the override keeps
    -- working past review_by (the same ruling as authorizations), but it
    -- surfaces on the overdue list until somebody looks at it.
    override_by     text,
    override_reason text,
    override_at     timestamptz,
    review_by       timestamptz,

    completed_at  timestamptz,
    CONSTRAINT overrides_have_a_reason_and_an_owner CHECK (
        override_at IS NULL OR
        (override_by IS NOT NULL AND override_reason IS NOT NULL AND review_by IS NOT NULL)
    )
);
-- One live recovery per person: a second one opened while the first is
-- undecided would let two panels answer the same question differently.
CREATE UNIQUE INDEX recoveries_one_open_per_party
    ON recoveries (party_id) WHERE state IN ('OPEN', 'CONFIRMED', 'OVERRIDDEN');
CREATE INDEX recoveries_overdue ON recoveries (review_by)
    WHERE review_by IS NOT NULL;

CREATE TABLE recovery_confirmations (
    recovery_id         text NOT NULL REFERENCES recoveries(id),
    confirmer_party_id  text NOT NULL,
    -- Who appointed the confirmer. The UNIQUE below is the anti-stacking rule
    -- in its strongest form: three confirmers all appointed by one
    -- organisation would be that organisation's decision wearing three names,
    -- so at most one confirmation per authority counts — enforced here, not in
    -- a handler that a second code path could skip.
    authority_party_id  text NOT NULL,
    confirmed_at        timestamptz NOT NULL,
    PRIMARY KEY (recovery_id, confirmer_party_id),
    UNIQUE (recovery_id, authority_party_id)
);
