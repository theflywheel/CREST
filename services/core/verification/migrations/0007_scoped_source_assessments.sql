-- A source assessment belongs to one registered source-system instance. The
-- adapter class is shared by many feeds and is retained only as a legacy key
-- for credentials issued before provenance carried systemRef.
ALTER TABLE source_assessments ADD COLUMN system_ref text;

-- Replace the adapter primary key with a surrogate so several current source
-- instances can use one adapter, while preserving one legacy adapter row per
-- class where system_ref remains NULL.
ALTER TABLE source_assessments DROP CONSTRAINT source_assessments_pkey;
ALTER TABLE source_assessments ADD COLUMN assessment_id bigserial;
ALTER TABLE source_assessments ADD CONSTRAINT source_assessments_pkey PRIMARY KEY (assessment_id);
CREATE UNIQUE INDEX source_assessments_system_ref_key
    ON source_assessments (system_ref)
    WHERE system_ref IS NOT NULL;
CREATE UNIQUE INDEX source_assessments_legacy_adapter_key
    ON source_assessments (adapter_ref)
    WHERE system_ref IS NULL;
