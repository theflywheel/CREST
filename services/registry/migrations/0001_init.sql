-- registry: who exists, under what terms, on which projects (Blueprint §3, §13).
--
-- Every primitive is stored as its canonical JSON document plus the columns
-- needed to find it. The document is the record; the columns are an index. That
-- way the schema in schemas/ stays the single definition of shape, and adding a
-- field is a schema change rather than a schema change plus a migration plus a
-- struct edit that someone forgets.

CREATE TABLE parties (
    id          text PRIMARY KEY,
    kind        text NOT NULL,
    doc         jsonb NOT NULL,
    created_at  timestamptz NOT NULL
);

-- The matching keys, one row per key a source system might join on (§4.1).
-- Separate from the document because matching is a lookup, and because "which
-- key matched" is provenance the claim has to carry.
--
-- national-id-hash values are salted hashes. The raw identifier is resolved at
-- ingestion and discarded; there is deliberately no column it could go in (W9).
CREATE TABLE party_keys (
    party_id    text NOT NULL REFERENCES parties(id) ON DELETE CASCADE,
    key_kind    text NOT NULL CHECK (key_kind IN ('national-id-hash', 'contact-route', 'roster-id')),
    key_value   text NOT NULL,
    -- Scope for roster ids: a programme roster id means nothing outside its
    -- project, and matching one globally is how two people become one person.
    scope_id    text,
    PRIMARY KEY (party_id, key_kind, key_value)
);
CREATE INDEX party_keys_lookup ON party_keys (key_kind, key_value);

CREATE TABLE terms (
    id            text NOT NULL,
    version       integer NOT NULL,
    doc           jsonb NOT NULL,
    published_at  timestamptz NOT NULL,
    PRIMARY KEY (id, version)
);

CREATE TABLE authorizations (
    id                  text PRIMARY KEY,
    party_id            text NOT NULL,
    scope_kind          text NOT NULL,
    context_id          text,
    functions           text[] NOT NULL,
    period_start        timestamptz NOT NULL,
    period_end          timestamptz,
    state               text NOT NULL,
    doc                 jsonb NOT NULL
);
CREATE INDEX authorizations_by_party ON authorizations (party_id, state);

CREATE TABLE contexts (
    id          text PRIMARY KEY,
    kind        text NOT NULL,
    state       text NOT NULL,
    doc         jsonb NOT NULL
);

-- Probable matches hold; they never auto-merge (W7). A hold is a row, so
-- "merges_without_confirmation = 0" is a query someone can run rather than an
-- aspiration someone can assert.
CREATE TABLE match_holds (
    id            text PRIMARY KEY,
    key_kind      text NOT NULL,
    key_value     text NOT NULL,
    candidates    text[] NOT NULL,
    reason        text NOT NULL,
    created_at    timestamptz NOT NULL,
    resolved_at   timestamptz,
    resolved_to   text
);
CREATE INDEX match_holds_open ON match_holds (resolved_at) WHERE resolved_at IS NULL;
