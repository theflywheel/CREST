-- definitions: what counts as done, in which version, per which face (§7, §13).
--
-- Versions are rows, never updates. "Immutable once ACTIVE" is enforced by the
-- service, and the table is shaped so that an accidental UPDATE would have to
-- be written on purpose: there is nowhere to put a change except a new row.

CREATE TABLE definitions (
    id             text NOT NULL,
    version        integer NOT NULL,
    state          text NOT NULL CHECK (state IN ('DRAFT', 'RATIFIED', 'ACTIVE', 'SUPERSEDED')),
    activity_code  text NOT NULL,
    authored_by    text NOT NULL,
    ratified_by    text,
    doc            jsonb NOT NULL,
    created_at     timestamptz NOT NULL,
    PRIMARY KEY (id, version),

    -- Separation of duties, at the one level that cannot be bypassed by a bug
    -- in a handler: author and approver on the same version must differ (§7).
    CONSTRAINT author_is_not_ratifier CHECK (ratified_by IS NULL OR ratified_by <> authored_by),
    -- A definition cannot leave DRAFT without someone having approved it.
    CONSTRAINT ratified_states_have_a_ratifier
        CHECK (state = 'DRAFT' OR ratified_by IS NOT NULL)
);
CREATE INDEX definitions_active ON definitions (activity_code, state);

-- LinkedRecords keyed to a definition — the rate lives here (§7: payment
-- set-ups link by reference, never embed, because a definition is complete and
-- usable with no rate attached).
CREATE TABLE definition_linked_records (
    id             text PRIMARY KEY,
    definition_id  text NOT NULL,
    type           text NOT NULL,
    version        integer NOT NULL,
    state          text NOT NULL,
    doc            jsonb NOT NULL,
    created_at     timestamptz NOT NULL
);
CREATE INDEX definition_linked_records_by_def ON definition_linked_records (definition_id, type, state);
