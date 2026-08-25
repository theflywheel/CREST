-- Closing a duplicate hold (§4, W7).
--
-- 0001 wrote the hold and gave it resolved_at/resolved_to to close with, and
-- nothing ever wrote them, for the same reason nothing wrote the unclear
-- queue's: listing a queue and working it are different features and only the
-- first was built. The consequence is worth naming plainly —
-- `merges_without_confirmation = 0` has been true since the day it was written,
-- because merging was impossible. A control that holds because the action does
-- not exist has not been tested; it has been avoided.
ALTER TABLE match_holds ADD COLUMN decision text
    CHECK (decision IN ('merge', 'distinct'));
ALTER TABLE match_holds ADD COLUMN resolved_by text;

-- Who confirmed, and how it was taken from them. §4 gives the merge decision to
-- the custodian *and* the worker's confirmation, and a confirmation nobody
-- recorded is a confirmation nobody took: this is the column that makes
-- `merges_without_confirmation` a query rather than an assertion.
ALTER TABLE match_holds ADD COLUMN confirmed_by text;
ALTER TABLE match_holds ADD COLUMN confirmation_method text;

-- A merge must carry a confirmation. `distinct` must not: saying "these are two
-- different people who share a phone number" takes nothing away from anybody
-- and asking a worker to confirm it would be asking them to ratify a fact about
-- somebody else.
ALTER TABLE match_holds ADD CONSTRAINT merges_are_confirmed
    CHECK (decision IS DISTINCT FROM 'merge'
           OR (confirmed_by IS NOT NULL AND confirmation_method IS NOT NULL));

-- A merged party is not deleted, and its identifier keeps resolving.
--
-- Deleting it would break every record that names it — claims, credentials,
-- payment instructions, all of which are in other services and none of which
-- this migration can reach. Worse, a worker's history would develop a hole
-- exactly where the system corrected its own mistake about who they were.
-- Instead the absorbed party stays, marked, pointing at the survivor.
ALTER TABLE parties ADD COLUMN merged_into text REFERENCES parties(id);
ALTER TABLE parties ADD COLUMN merged_at timestamptz;

-- A party cannot be merged into itself, and a survivor cannot itself be merged
-- away by the same statement that absorbs somebody into it. Chains are allowed
-- and followed on read; a cycle is not reachable through the resolution
-- endpoint, which refuses a survivor that is already merged.
ALTER TABLE parties ADD CONSTRAINT merge_is_not_self
    CHECK (merged_into IS NULL OR merged_into <> id);
CREATE INDEX parties_merged ON parties (merged_into) WHERE merged_into IS NOT NULL;
