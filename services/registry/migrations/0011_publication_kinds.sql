-- Converge the subject_kind CHECK with what the code publishes (#72, #77).
--
-- What happened, so it does not happen again: 0002 created this constraint
-- with three kinds, and was then edited IN PLACE — #72 added 'instance', #77
-- added 'skill'. The migration runner applies a file once by name, so every
-- fresh database (CI, local) got the five-kind constraint and the deployed
-- database kept the three-kind one. The visible symptom was invisible until
-- somebody read the logs: the startup instance publication (#70) enqueued a
-- fact production's constraint refuses, and the outbox retried it forever —
-- 316 attempts on the oldest row when this was found. Nothing failed loudly,
-- because at-least-once delivery treats "not yet" as normal.
--
-- The rule this writes down: a migration that has shipped is history, not a
-- document to edit. Changes go in a new file like this one.
ALTER TABLE registry_publications
    DROP CONSTRAINT registry_publications_subject_kind_check;
ALTER TABLE registry_publications
    ADD CONSTRAINT registry_publications_subject_kind_check
    CHECK (subject_kind IN ('organisation', 'terms', 'authorization', 'instance', 'skill'));
