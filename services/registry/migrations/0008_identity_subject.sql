-- Binding an authenticated subject to a Party (#89, §4.1).
--
-- Until now `party_keys` held the three joining identifiers evidence arrives
-- with: a hashed national id, a contact route, a roster id. Those answer "whose
-- work is this?". This adds a fourth kind that answers a different question —
-- "who is making this request?" — and the two must not be confused, which is
-- why it is a new kind rather than a fourth entry in resolve()'s precedence
-- list. An identity subject must never join a CSV row to a worker: the token
-- proves who is at the keyboard, not who was in the village.
ALTER TABLE party_keys DROP CONSTRAINT party_keys_key_kind_check;
ALTER TABLE party_keys ADD CONSTRAINT party_keys_key_kind_check
    CHECK (key_kind IN ('national-id-hash', 'contact-route', 'roster-id', 'identity-subject'));

-- Backfilled from the bindings already recorded. Every identityBindings entry
-- carries a subjectRef and always has; what was missing was an index that could
-- go the other way, from a subject to the party it belongs to.
--
-- Only the three classes a token can come from. `document-seen` and `asserted`
-- also carry a subjectRef — an agent's note of a document number, a locally
-- invented reference — and neither is something anybody can present a token
-- for; letting those in would mean a party could be logged in as by whoever
-- knew the number on their card. `mobile-otp` is left out for the same reason:
-- proving control of a phone is not an OIDC session, and its subjectRef is not
-- a `sub` anybody's issuer will ever assert.
--
-- If this backfill and the index below fail together, it is because one subject
-- already sits on two parties. That is a real duplicate and the migration
-- stopping is correct: it needs the hold queue and a person, not a rule that
-- silently keeps whichever row was written first.
INSERT INTO party_keys (party_id, key_kind, key_value, scope_id)
SELECT p.id, 'identity-subject', b->>'subjectRef', NULL
FROM parties p, jsonb_array_elements(COALESCE(p.doc->'identityBindings', '[]'::jsonb)) AS b
WHERE b->>'providerClass' IN ('esignet', 'mosip-ida', 'generic-oidc')
  AND COALESCE(b->>'subjectRef', '') <> ''
ON CONFLICT DO NOTHING;

-- One subject, one party. This is the same rule as the national identifier's
-- and it is enforced here rather than only in Go, because the consequence of
-- losing it is that a token authenticates as two people and the service picks
-- one — which is a worker acting as a stranger with nothing in the record to
-- show it happened.
--
-- Note what it does *not* say: a party may hold several subjects. Somebody who
-- re-binds after changing phones has two, and both are true.
CREATE UNIQUE INDEX party_keys_one_party_per_subject
    ON party_keys (key_value) WHERE key_kind = 'identity-subject';
