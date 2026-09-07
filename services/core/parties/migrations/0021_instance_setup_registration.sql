-- The first-run operator is established by the configured deployment
-- administrator, whose identity is recorded privately in instance_setup and
-- is intentionally not a Party that could be named as a second approver.
-- Keep ordinary registry approvals subject to the non-self-approval rule,
-- while linking this one special admission to its durable setup decision.
ALTER TABLE instance_setup
    ADD CONSTRAINT instance_setup_instance_operator_unique
    UNIQUE (instance_id, operator_party_id);

ALTER TABLE org_registrations
    ADD COLUMN decision_source text NOT NULL DEFAULT 'REGISTRY',
    ADD COLUMN setup_instance_id text,
    ADD CONSTRAINT org_registration_decision_source_check CHECK (
        (decision_source = 'INSTANCE_SETUP') = (setup_instance_id IS NOT NULL)
    ),
    ADD CONSTRAINT org_registration_setup_instance_fk
        FOREIGN KEY (setup_instance_id, party_id)
        REFERENCES instance_setup (instance_id, operator_party_id);

ALTER TABLE org_registrations
    DROP CONSTRAINT approval_is_not_self_granted;

ALTER TABLE org_registrations
    ADD CONSTRAINT approval_is_not_self_granted CHECK (
        state <> 'APPROVED'
        OR (
            decision_source = 'INSTANCE_SETUP'
            AND decided_by = party_id
            AND setup_instance_id IS NOT NULL
        )
        OR (
            decision_source = 'REGISTRY'
            AND setup_instance_id IS NULL
            AND decided_by IS DISTINCT FROM party_id
        )
    );

ALTER TABLE org_registrations
    ADD CONSTRAINT org_registration_setup_source_check CHECK (
        decision_source <> 'INSTANCE_SETUP'
        OR (state = 'APPROVED' AND decided_by = party_id)
    );
