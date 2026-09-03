-- Finding, fixed: #189 named confidence-check a valid assisted-enrolment
-- method (w1_4's no-document route — a provenance fact, never a stored
-- judgement) in the handler and the OpenAPI, but 0003's check constraint on
-- assisted_enrolments.method still admitted only the original three, so the
-- FIRST real confidence-check enrolment failed the INSERT and 500'd. Widened
-- to match validEnrolmentMethod (onboarding.go) — the same list, in the same
-- order, one source of truth short of ideal but now at least agreeing.
ALTER TABLE assisted_enrolments DROP CONSTRAINT assisted_enrolments_method_check;
ALTER TABLE assisted_enrolments
    ADD CONSTRAINT assisted_enrolments_method_check
    CHECK (method IN ('supervisor-attested', 'roster-import', 'field-visit', 'confidence-check'));
