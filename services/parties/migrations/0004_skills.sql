-- The skill list (#16, Blueprint §3).
--
-- Reference data, not a primitive. §3 files it beside credential shapes and
-- adapters — "published releases, adopted by deployments" — rather than beside
-- Party and Definition. The eleven primitives describe what happened; this
-- describes a vocabulary several deployments have to agree on for a worker's
-- record to mean the same thing when they cross a border.
--
-- FINDING, recorded rather than worked around: §3 says the canonical copy lives
-- on the GLOBAL DeDi node and national nodes mirror it. CREST has no global
-- node. So this deployment publishes its skill list to its own node, which
-- makes it a taxonomy nobody else shares — the opposite of what a skill code is
-- for. It is still worth having: the codes are stable and in credentials from
-- day one, so adopting a shared list later is a mirroring problem rather than a
-- reissuance problem, which it would be if credentials carried no code at all.

CREATE TABLE skills (
    -- The version is inside the code (…​.v2), so this is the whole identity. A
    -- revised skill is a new row, never an edited one: the old code is in
    -- credentials that cannot be rewritten, and a code whose meaning changed
    -- underneath them would silently restate what a worker's record says they
    -- can do.
    code          text PRIMARY KEY,
    label         text NOT NULL,
    description   text NOT NULL DEFAULT '',
    supersedes    text REFERENCES skills(code),
    published_at  timestamptz NOT NULL,

    CONSTRAINT a_skill_does_not_supersede_itself CHECK (supersedes IS DISTINCT FROM code)
);
CREATE INDEX skills_superseded ON skills (supersedes) WHERE supersedes IS NOT NULL;
