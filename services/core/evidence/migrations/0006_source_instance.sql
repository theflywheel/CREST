-- A project may receive evidence from several source-system instances through
-- the same connector. Each instance has its own mapping, cadence and owner.
ALTER TABLE sources DROP CONSTRAINT IF EXISTS sources_adapter_ref_context_id_key;
ALTER TABLE sources ADD CONSTRAINT sources_connector_instance_unique
    UNIQUE (adapter_ref, context_id, system_ref);
