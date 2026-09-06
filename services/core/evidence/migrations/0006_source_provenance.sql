-- Approved provenance belongs to the registered source, not to a batch query.
-- Existing installations receive empty values and must re-register a source
-- before it can ingest again; no historical record is rewritten.
ALTER TABLE sources ADD COLUMN source_class text NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN capture_method text NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN source_exposure text NOT NULL DEFAULT '';

-- Older deployments keyed feeds by adapter class and context, which collapsed
-- two source-system instances. The source system reference is the stable feed
-- identity; one adapter class can translate many feeds in one context.
ALTER TABLE sources DROP CONSTRAINT IF EXISTS sources_adapter_ref_context_id_key;
ALTER TABLE sources ADD CONSTRAINT sources_context_system_ref_key UNIQUE (context_id, system_ref);
