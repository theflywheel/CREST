-- Projects: the J3 setup surface, on the Context primitive (§2, §13).
--
-- A "project" is not a new primitive. Context already is "a bounded
-- operational scope with its own configuration and activation gates", and §2
-- says so in as many words: project and campaign are what profiles call it.
-- So this migration adds no contexts table twin — it adds the columns the
-- project queries need as an index over the document, exactly as 0001 did for
-- parties and authorizations, plus two tables for facts a context carries that
-- are not part of the primitive's document.
--
-- What is deliberately NOT here: coverage, definition origin, payment posture,
-- finance code formats, role names, sector taxonomies. Two deployments could
-- reasonably disagree about every one of those and both still be CREST, so
-- they live in Context.configuration and in the opaque payloads below, and no
-- column here has an opinion about them.

-- Index columns over contexts.doc. The document stays the record.
ALTER TABLE contexts ADD COLUMN owner_party_id        text;
ALTER TABLE contexts ADD COLUMN configurator_party_id text;
ALTER TABLE contexts ADD COLUMN ownership_state       text;

-- Contexts that predate this migration carry an owner in their document; the
-- index column is filled from it rather than left null and quietly excluded
-- from every listing.
UPDATE contexts SET owner_party_id = doc->>'ownerPartyId';

CREATE INDEX contexts_by_owner        ON contexts (owner_party_id, kind);
CREATE INDEX contexts_by_configurator ON contexts (configurator_party_id, ownership_state);

-- The ownership acknowledgement trail (design finding F2).
--
-- Context.ownership holds the current answer; this table holds every answer
-- there has ever been. Append-only on purpose: a re-handover after a decline
-- overwrites the current view, and "who declined this and why" is exactly the
-- fact an Org Admin needs after it stops being current. A single mutable
-- column would lose it, and on a system whose records decide whether somebody
-- gets paid, losing the record of a refusal is the failure this table exists
-- to prevent.
CREATE TABLE context_ownership_events (
    context_id  text NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
    seq         integer NOT NULL,
    -- NAMED, ACCEPTED or DECLINED. The verbs of the acknowledgement, not the
    -- context's own DRAFT/ACTIVE/CLOSED lifecycle.
    event       text NOT NULL CHECK (event IN ('NAMED', 'ACCEPTED', 'DECLINED')),
    -- The party the event is about: the named configurator.
    party_id    text NOT NULL,
    -- The party who did it: the Org Admin who named, or the configurator who
    -- answered. Never derived from the other column.
    actor_party_id text NOT NULL,
    reason      text,
    at          timestamptz NOT NULL,
    PRIMARY KEY (context_id, seq)
);

-- Configuration-level records keyed to a context.
--
-- One generic table rather than one table per screen, because the shape is the
-- same in every case and only the vocabulary differs: something was recorded
-- about this context, under a name, by somebody, at a time. The composition
-- choices (p2_1, p2_3, p2_5), the finance-code link (p2_8) and the support
-- owner (p2_10) are all that shape. `kind` and `payload` are opaque here — the
-- core stores and returns them and never reads inside, which is the same
-- contract Context.configuration already carries.
--
-- CREST does not invent account codes: a finance-code payload is a link to a
-- code some finance system already minted, and there is deliberately no
-- sequence, no format and no generator anywhere near this table.
CREATE TABLE context_records (
    context_id  text NOT NULL REFERENCES contexts(id) ON DELETE CASCADE,
    kind        text NOT NULL,
    payload     jsonb NOT NULL,
    recorded_by text NOT NULL,
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (context_id, kind)
);
