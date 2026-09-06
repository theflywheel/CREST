-- Bind every new authoring draft to the project whose definition it changes.
-- Existing drafts retain an empty context for migration compatibility; the
-- handler refuses that value for new mutations unless an instance grant is
-- explicitly being used for a legacy definition.
ALTER TABLE definition_drafts ADD COLUMN context_id text NOT NULL DEFAULT '';
CREATE INDEX definition_drafts_by_context ON definition_drafts (context_id, state);

