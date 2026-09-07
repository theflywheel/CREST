-- Source assessments written before the reference tier numbering used 1 as
-- strongest. Preserve the judgement and translate only its representation:
-- legacy 1/2/3 (strongest to weakest) becomes reference 3/2/1. Legacy 0 was
-- not an actionable cap; retain a conservative weakest cap until reviewed.
UPDATE source_assessments
   SET max_tier = CASE
       WHEN max_tier BETWEEN 1 AND 3 THEN 4 - max_tier
       ELSE 3
   END;
ALTER TABLE source_assessments DROP CONSTRAINT source_assessments_max_tier_check;
ALTER TABLE source_assessments
    ADD CONSTRAINT source_assessments_max_tier_check CHECK (max_tier BETWEEN 1 AND 3);
