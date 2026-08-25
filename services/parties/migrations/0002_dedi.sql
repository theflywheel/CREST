-- Publication of public facts to DeDi (#20, Blueprint §3).
--
-- §3's placement rule is the whole point of this migration: public facts —
-- which organisations exist, which terms they hold, which authorizations were
-- granted — go to a transparency log; personal data stays here. A verifier has
-- to be able to establish which organisation held which terms at issuance time
-- without asking CREST, and until now they could not.
--
-- Nothing in the tables below holds personal data. What reaches the node is
-- decided by hand-written projections in publish.go, not by serialising a row.

CREATE TABLE outbox (
    id              bigserial PRIMARY KEY,
    topic           text NOT NULL,
    payload         jsonb NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    attempts        integer NOT NULL DEFAULT 0,
    claimed_at      timestamptz,
    delivered_at    timestamptz,
    next_attempt_at timestamptz,
    last_error      text
);
CREATE INDEX outbox_pending ON outbox (id) WHERE delivered_at IS NULL;

-- Where each published fact landed. subject_kind names which registry it went
-- to, so "prove this organisation held these terms in March" is one query
-- rather than three.
CREATE TABLE registry_publications (
    subject_kind     text NOT NULL CHECK (subject_kind IN ('organisation', 'terms', 'authorization', 'instance', 'skill')),
    subject_id       text NOT NULL,
    subject_version  integer NOT NULL DEFAULT 1,
    namespace        text NOT NULL,
    registry         text NOT NULL,
    record           text NOT NULL,
    registry_version text NOT NULL,
    digest           text NOT NULL,
    state            text NOT NULL,
    -- Whether a real transparency log was behind this publication. Stored per
    -- row because a deployment can move from the fallback to a node, and a
    -- question about a fact published last month needs last month's answer.
    transparent      boolean NOT NULL,
    published_at     timestamptz NOT NULL,
    PRIMARY KEY (subject_kind, subject_id, subject_version)
);

-- The Postgres fallback's store (#20: DeDi-node is at M1). Append-only, and
-- honest about carrying no inclusion proof.
CREATE TABLE dedi_records (
    namespace   text NOT NULL,
    registry    text NOT NULL,
    record      text NOT NULL,
    version     integer NOT NULL,
    digest      text NOT NULL,
    state       text NOT NULL,
    details     jsonb NOT NULL,
    created_at  timestamptz NOT NULL,
    PRIMARY KEY (namespace, registry, record, version)
);
