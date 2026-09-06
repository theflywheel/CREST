-- Keep the request identity beside a credential so idempotent retries remain
-- scoped after custody removes the signed document. Existing rows are not
-- rewritten: unit and route can be recovered from retained signed documents,
-- while context is intentionally left NULL where the old document never
-- carried it. Such a legacy row fails closed on a retry rather than accepting
-- a caller-supplied context without evidence.
ALTER TABLE credentials ADD COLUMN IF NOT EXISTS unit_id text;
ALTER TABLE credentials ADD COLUMN IF NOT EXISTS context_id text;
ALTER TABLE credentials ADD COLUMN IF NOT EXISTS confirmation_route text;

UPDATE credentials
   SET unit_id = COALESCE(unit_id, doc #>> '{credentialSubject,workEvent,eventId}'),
       confirmation_route = COALESCE(confirmation_route,
                                     doc #>> '{credentialSubject,confirmation,route}')
 WHERE doc IS NOT NULL;
