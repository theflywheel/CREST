-- A re-submitted batch must not pay anyone twice.
--
-- The original schema said so in a comment and did not enforce it. Claims were
-- unique on (unit_id, party_id), but the unit id was minted fresh on every
-- ingestion — so the same CSV submitted twice produced two units, two claims,
-- two windows, two credentials and two payments. Nothing in the schema could
-- notice, because from the database's point of view they were different work.
--
-- A source system retrying after a client-side timeout is not an unusual event.
-- It is the ordinary case, and it is exactly the case that produced a double
-- payment.
--
-- The fix gives a unit an identity derived from what it *describes* rather than
-- from when it was written: the context, the definition, the activity, the
-- worker's joining identifier, the period and the outcome. Two ingestions of
-- the same row now converge on one unit, and the existing (unit_id, party_id)
-- constraint on claims does the rest.
ALTER TABLE units ADD COLUMN dedupe_key text;

-- Backfilled as the unit's own id for anything already stored: existing rows
-- have no stable key to recompute, and inventing one would merge units that
-- were legitimately separate.
UPDATE units SET dedupe_key = id WHERE dedupe_key IS NULL;

ALTER TABLE units ALTER COLUMN dedupe_key SET NOT NULL;
CREATE UNIQUE INDEX units_dedupe ON units (dedupe_key);
