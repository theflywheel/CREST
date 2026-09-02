-- Recovery contacts and refusals (§16, J7/J10; screens w1_7, w4_1–w4_3).
--
-- A nomination is ROUTING, not eligibility. The 2-of-3 rule deliberately
-- accepts any two vouched voices from distinct authorities (0010_recovery.sql:
-- "of three is not a pre-named panel"), because naming three people in advance
-- is hardest exactly where recovery is needed most. What a nomination adds is
-- the worker's own answer to "who should be asked first": the contacts a
-- recovery request is routed to, and the people whose pending-request view
-- shows it. A nominated contact who is not vouched for by any authority still
-- cannot confirm — nomination never widens who may speak.
--
-- The contact is a PARTY, never a phone number. A worker nominates a person
-- they trust; how that person is reached lives on that person's own contact
-- routes. Storing a raw number here would make a phone number an identity,
-- which it is not — numbers are re-issued, shared, and lost with the handset,
-- which is the exact failure recovery exists for.
CREATE TABLE recovery_contacts (
    party_id          text NOT NULL REFERENCES parties(id),
    contact_party_id  text NOT NULL REFERENCES parties(id),
    nominated_by      text NOT NULL,
    nominated_at      timestamptz NOT NULL,
    -- Revocation marks, never deletes: who a worker trusted, and when they
    -- stopped, is what a later dispute about a recovery is answered from.
    revoked_at        timestamptz,
    CONSTRAINT contact_is_someone_else CHECK (party_id <> contact_party_id)
);
-- One live nomination per pair. Re-nominating after a revocation is a new row.
CREATE UNIQUE INDEX recovery_contacts_one_live_per_pair
    ON recovery_contacts (party_id, contact_party_id) WHERE revoked_at IS NULL;
CREATE INDEX recovery_contacts_by_contact
    ON recovery_contacts (contact_party_id) WHERE revoked_at IS NULL;

-- A refusal is a recorded answer, not a dead end (w4_3). The reference design
-- draws the refusal screen and defines nothing after it; the defined path is:
-- the refusal is written with its owner (the refuser) and a required reason,
-- the recovery stays OPEN — a refusal is one voice, and any two other vouched
-- voices may still carry it — and every refused-but-undecided recovery
-- surfaces on GET /v1/recoveries?refused=true with the opener named as the
-- person who owes it a next step. Same posture as a held payment: a state
-- somebody owns, never a silence.
CREATE TABLE recovery_refusals (
    recovery_id        text NOT NULL REFERENCES recoveries(id),
    refuser_party_id   text NOT NULL,
    reason             text NOT NULL CHECK (reason <> ''),
    refused_at         timestamptz NOT NULL,
    PRIMARY KEY (recovery_id, refuser_party_id)
);
