-- Finding, fixed: #189 taught the collect path to write a presentation row
-- with scope='consented' ("who checked me" reads a consented share like any
-- other check, §9/W8) and updated the OpenAPI enum — but 0001's check
-- constraint still admitted only ('bare','scoped'), so the FIRST real
-- consented collect failed the INSERT and the whole collect 500'd. The unit
-- tests exercise the pure state machine, not this table, which is how the gap
-- survived to the end-to-end walk that found it.
ALTER TABLE presentations DROP CONSTRAINT presentations_scope_check;
ALTER TABLE presentations
    ADD CONSTRAINT presentations_scope_check
    CHECK (scope IN ('bare', 'scoped', 'consented'));
