-- A hold carries the project scope used to authorize its custodian.
ALTER TABLE match_holds ADD COLUMN context_id text REFERENCES contexts(id);

-- Worker confirmation is a separate append-only fact. The resolver cannot
-- manufacture it by copying a name from its own request body.
CREATE TABLE match_hold_confirmations (
    hold_id             text NOT NULL REFERENCES match_holds(id),
    survivor_party_id   text NOT NULL REFERENCES parties(id),
    confirmed_by        text NOT NULL REFERENCES parties(id),
    confirmation_method text NOT NULL,
    evidence_ref        text,
    confirmed_at        timestamptz NOT NULL,
    PRIMARY KEY (hold_id, survivor_party_id)
);

CREATE INDEX match_hold_confirmations_worker
    ON match_hold_confirmations (confirmed_by, confirmed_at);

CREATE FUNCTION reject_hold_confirmation_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'match hold confirmations are append-only';
END;
$$;

CREATE TRIGGER match_hold_confirmations_immutable
    BEFORE UPDATE OR DELETE ON match_hold_confirmations
    FOR EACH ROW EXECUTE FUNCTION reject_hold_confirmation_mutation();
