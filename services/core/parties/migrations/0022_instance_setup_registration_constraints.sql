-- Keep the source marker closed to its two meanings and preserve the
-- non-self-decision invariant for ordinary rejections as well as approvals.
ALTER TABLE org_registrations
    ADD CONSTRAINT org_registration_decision_source_value_check CHECK (
        decision_source IN ('REGISTRY', 'INSTANCE_SETUP')
    );

ALTER TABLE org_registrations
    DROP CONSTRAINT approval_is_not_self_granted;

ALTER TABLE org_registrations
    ADD CONSTRAINT approval_is_not_self_granted CHECK (
        (
            decision_source = 'INSTANCE_SETUP'
            AND state = 'APPROVED'
            AND decided_by = party_id
            AND setup_instance_id IS NOT NULL
        )
        OR (
            decision_source = 'REGISTRY'
            AND setup_instance_id IS NULL
            AND decided_by IS DISTINCT FROM party_id
        )
    );
