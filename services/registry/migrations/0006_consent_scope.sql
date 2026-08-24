-- Enrolment consent is scoped to a context, not to the deployment (#24).
--
-- 0005 recorded one enrolment consent per party for the whole deployment,
-- reading §9's "recorded once" as once per person. That is wrong, and it is
-- wrong in the direction that costs a worker something: it meant a worker who
-- agreed to one programme holding their work history had thereby agreed to
-- every other organisation on the same deployment holding it too. Nobody asked
-- them that, and consent nobody asked for is not consent.
--
-- "Recorded once" means once per relationship. A worker who joins a second
-- programme is asked again, because it is a second question.
ALTER TABLE consents ADD COLUMN context_id text REFERENCES contexts(id) ON DELETE RESTRICT;

-- Any enrolment consent already recorded has no context, and there is no
-- honest value to give it: choosing one would be inventing a scope the worker
-- never agreed to, and treating it as covering everything is precisely the bug
-- being fixed. Such rows exist only from local development and harness runs
-- during the hours between 0005 and this migration, so they are removed rather
-- than reinterpreted.
--
-- If this ever runs somewhere with real rows it will delete real consent
-- records, and that is the correct failure: a consent whose scope nobody can
-- state is not one to keep acting on. It leaves the worker at NONE, which is
-- permissive, so nothing stops counting.
DELETE FROM consents WHERE moment = 'enrolment' AND context_id IS NULL;

ALTER TABLE consents ADD CONSTRAINT enrolment_consent_names_its_programme
    CHECK (moment <> 'enrolment' OR context_id IS NOT NULL);

-- One live enrolment consent per party PER CONTEXT, replacing the
-- deployment-wide index.
DROP INDEX consents_one_live_enrolment;
CREATE UNIQUE INDEX consents_one_live_enrolment_per_context
    ON consents (party_id, context_id) WHERE moment = 'enrolment' AND revoked_at IS NULL;

CREATE INDEX consents_by_party_and_context ON consents (party_id, context_id, moment);
