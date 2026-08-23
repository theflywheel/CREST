-- Source heartbeat monitoring (#22, Blueprint §8).
--
-- The failure this exists for is the one a worker cannot report.
--
-- Every other way this system fails leaves the worker something to point at: a
-- held payment has a reason, a rejected row lands in the unclear queue, a wrong
-- record can be disputed. A source system going quiet produces nothing at all.
-- The worker keeps working, their record simply stops growing, and from where
-- they stand that is indistinguishable from having done no work — so they have
-- nothing to dispute and no reason to think anything is wrong. Nobody finds out
-- until a payment cycle comes up short, by which time the evidence of the work
-- is weeks old and in a system nobody is watching.
--
-- So silence has to be a thing this system notices on its own.

CREATE TABLE sources (
    id              text PRIMARY KEY,
    -- What is being watched: an adapter, feeding a project. The same adapter
    -- serving two projects is two sources, because it can go quiet for one and
    -- not the other, and a single row would average that away.
    adapter_ref     text NOT NULL,
    context_id      text NOT NULL,
    system_ref      text NOT NULL,

    -- How often evidence is expected. Declared, never inferred: a cadence
    -- guessed from history would call a source healthy precisely when it has
    -- been quietly degrading, because the history it learns from is the
    -- degraded one.
    expected_every  interval NOT NULL CHECK (expected_every > interval '0'),

    -- Who is answerable when it stops. The same shape as a held payment's
    -- reason: an alert with no owner is an alert that gets forwarded until it
    -- is nobody's.
    owner_party_id  text NOT NULL,

    registered_at   timestamptz NOT NULL,
    last_seen_at    timestamptz,

    -- When the current silence began, and whether its owner has been told.
    -- Both nullable because both are absent while the source is healthy.
    --
    -- silent_since is what makes the alert fire once per episode rather than
    -- once per sweep. The state itself is derived from last_seen_at and the
    -- clock and is never stored — a stored state is wrong the moment the clock
    -- moves, and this table is read by a sweep that exists to notice time
    -- passing.
    silent_since    timestamptz,
    notified_at     timestamptz,

    UNIQUE (adapter_ref, context_id)
);
CREATE INDEX sources_watching ON sources (last_seen_at);
