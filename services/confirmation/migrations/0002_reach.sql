-- Auto-confirmation must not be applied to a worker nobody reached.
--
-- The original sweep selected on `exit_route IS NULL AND closes_at <= now` and
-- nothing else. `notified_at` was set when the notification was *queued*, not
-- when it landed, and notify answers 201 for a send that failed and for a
-- worker with no reachable route at all. So a worker whose SMS never arrived —
-- or who has no phone — had their record auto-confirmed against them after
-- seven days of a silence the system manufactured.
--
-- That is the worst version of this failure, because nothing surfaces it. A
-- held payment has a reason and an owner; an unreached worker had neither.
ALTER TABLE windows ADD COLUMN reach text
    CHECK (reach IS NULL OR reach IN ('reached', 'unreached'));
ALTER TABLE windows ADD COLUMN reach_detail text;
ALTER TABLE windows ADD COLUMN escalated_at timestamptz;

-- The sweep reads this, so it is worth being able to find.
CREATE INDEX windows_unreached ON windows (reach) WHERE exit_route IS NULL;
