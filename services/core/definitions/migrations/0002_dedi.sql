-- Publication to DeDi on ratification (#21, Blueprint §3 and §7).
--
-- The half of a definition a credential actually depends on. A credential pins
-- the definition version it was issued against; if that version only exists in
-- this service's Postgres, a verifier can resolve it only by asking CREST, and
-- "ask the issuer whether the issuer is right" is not verification.

-- The outbox. definitions had none because it had no side effects; publishing
-- is one, and it is the kind that must not be lost. A definition that went
-- ACTIVE and was never published is a definition credentials will be issued
-- against and no verifier can resolve — so the publish is enqueued in the same
-- transaction as the state change, not attempted after it.
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

-- Where a published version landed, written back when the publish succeeds.
--
-- transparent records whether a real transparency log was behind it. It is a
-- column rather than a deployment-wide setting because a deployment can move
-- from the Postgres fallback to a node, and a verifier asking about a
-- definition published last month needs the answer for last month.
CREATE TABLE definition_publications (
    definition_id  text NOT NULL,
    version        integer NOT NULL,
    namespace      text NOT NULL,
    registry       text NOT NULL,
    record         text NOT NULL,
    registry_version text NOT NULL,
    digest         text NOT NULL,
    state          text NOT NULL,
    transparent    boolean NOT NULL,
    published_at   timestamptz NOT NULL,
    PRIMARY KEY (definition_id, version)
);

-- The Postgres fallback's store, used only when DEDI_URL is unset (#20 asks for
-- one because DeDi-node is at M1). Append-only: there is no UPDATE path in
-- pkg/dedi, and an old version stays resolvable after a new one supersedes it.
-- What it cannot offer is an inclusion proof, and pkg/dedi says so rather than
-- pretending otherwise.
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
