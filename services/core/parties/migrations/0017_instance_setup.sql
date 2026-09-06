CREATE TABLE instance_setup (
    instance_id text PRIMARY KEY,
    operator_party_id text NOT NULL REFERENCES parties(id),
    administrator_subject text NOT NULL,
    administrator_issuer text NOT NULL,
    completed_at timestamptz NOT NULL
);
