-- Definition authoring (P-3): drafts and the governance event record.
--
-- The definitions table is create-once — a version is immutable from the
-- moment it exists, and "create" never means "replace" (see ErrAlreadyExists).
-- Authoring needs the opposite: a document a person edits section by section
-- over days. Those are two different objects, so the draft is its own table
-- rather than a mutable state smuggled into the immutable one. Submitting a
-- draft compiles it into the next definitions row; nothing ever flows back.
CREATE TABLE definition_drafts (
    id            text PRIMARY KEY,
    -- The definition this draft will become a version of. Set when cloning
    -- from an existing definition; NULL for wholly new work until submit
    -- mints the id.
    definition_id text,
    base_version  integer,
    -- OPEN → SUBMITTED | DISCARDED. A submitted draft is kept, not deleted:
    -- it is the provenance of the version it produced.
    state         text NOT NULL DEFAULT 'OPEN',
    doc           jsonb NOT NULL,
    created_by    text NOT NULL,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    -- The definitions row this draft became, written at submit.
    submitted_version integer
);
CREATE INDEX definition_drafts_open ON definition_drafts (created_at) WHERE state = 'OPEN';

-- Append-only governance record: who did what to which version, when.
-- Ratification naming pending fields, activation, submission — each is a row
-- with an actor, and there is no UPDATE or DELETE path in this service.
-- The definition document shows the current state; this table shows how it
-- got there, which is the part a dispute needs.
CREATE TABLE definition_events (
    id            bigserial PRIMARY KEY,
    definition_id text NOT NULL,
    version       integer NOT NULL,
    action        text NOT NULL,
    actor_party_id text NOT NULL,
    at            timestamptz NOT NULL,
    detail        jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX definition_events_by_definition ON definition_events (definition_id, version, id);
