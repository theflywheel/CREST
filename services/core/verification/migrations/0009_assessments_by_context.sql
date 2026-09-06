-- A source is registered per project — the evidence registry keys it by
-- (context_id, system_ref) — so an assessment of it is per project too. Keyed
-- by system_ref alone, downgrading "csv-batch" in one project silently capped
-- every other project's credentials from a source that merely shared the name.
ALTER TABLE source_assessments ADD COLUMN context_id text;

-- Rows assessed before scoping carry no context. They are kept and keep
-- applying to every project (the conservative reading: a recorded concern is
-- not lifted by a schema change) until an operator re-assesses or clears them
-- in a project, at which point the scoped row takes precedence.
DROP INDEX IF EXISTS source_assessments_system_ref_key;
CREATE UNIQUE INDEX source_assessments_scoped_key
    ON source_assessments (context_id, system_ref)
    WHERE system_ref IS NOT NULL AND context_id IS NOT NULL;
CREATE UNIQUE INDEX source_assessments_unscoped_system_ref_key
    ON source_assessments (system_ref)
    WHERE system_ref IS NOT NULL AND context_id IS NULL;
