-- Review-by dates on authorizations (§16, Tier 2).
--
-- The gap this closes: nothing in the system ever expired or got re-reviewed,
-- so a qualification approved once was current forever. The ruling is "flag
-- overdue, keep working": passing review_by changes no behaviour — permits()
-- still answers yes — it makes the authorization visibly overdue in a list an
-- administrator reads and as a flag on the permits answer. The alternative,
-- lapsing at the date, was rejected because a missed calendar entry would
-- withhold a worker's payment, colliding with "every held payment has a
-- reason with an owner".
--
-- A column rather than a doc-only field because "which authorizations are
-- overdue" is a query someone runs, and a query over jsonb is a query nobody
-- runs.
ALTER TABLE authorizations ADD COLUMN review_by timestamptz;
UPDATE authorizations SET review_by = (doc ->> 'reviewBy')::timestamptz
 WHERE doc ? 'reviewBy';
CREATE INDEX authorizations_overdue ON authorizations (review_by)
 WHERE review_by IS NOT NULL AND state = 'ACTIVE';
