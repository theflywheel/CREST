-- Why an unclear row landed here, as a value rather than as prose (#25).
--
-- The `reason` column is written for a person to read, and it has always been
-- the only thing distinguishing one unclear row from another. That was fine
-- while the queue could only be listed. It stops being fine the moment a row
-- can be re-attributed, because only one of the reasons is re-attributable:
--
--   unattributed       nobody matched the identifier, or the registry is
--                      holding an ambiguous match. The record itself is sound
--                      and the only missing fact is who did the work.
--   contract           the record failed the canonical evidence contract, or
--                      described an activity or an outcome unit the definition
--                      does not count. Attaching a person to it would smuggle
--                      a record that failed the contract into the ledger.
--   rejected           the adapter refused the row and never parsed it, so
--                      there is nothing to attach a person to.
--   consent-withdrawn  the worker asked us to stop collecting evidence about
--                      them (§9). Resolving it would be recording it anyway.
--
-- Matching on the prose instead would work until someone reworded a message.
ALTER TABLE unclear_rows ADD COLUMN kind text NOT NULL DEFAULT 'unknown';

-- Rows written before this column existed keep 'unknown' and are not
-- re-attributable. Backfilling them by parsing their reason strings would be a
-- guess, and a guess here attaches work to a person.
COMMENT ON COLUMN unclear_rows.kind IS
    'unattributed | contract | rejected | consent-withdrawn | unknown (pre-0005)';

-- Who re-attributed the row, alongside when and to whom. resolved_to has been
-- there since 0001 and nothing ever wrote it; a resolution with no named
-- resolver is the same shape of unaccountable as a held payment with no owner.
ALTER TABLE unclear_rows ADD COLUMN resolved_by text;
ALTER TABLE unclear_rows ADD COLUMN resolution_claim_id text;
