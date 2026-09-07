-- Durable local provider state. It is a simulator only: Submit inserts PENDING
-- and never transitions this row to confirmed.
CREATE TABLE payment_simulator_transfers (
    idempotency_key       text PRIMARY KEY,
    instruction_id        text NOT NULL,
    context_id            text NOT NULL,
    reference             text NOT NULL,
    amount_minor          bigint NOT NULL,
    currency              text NOT NULL,
    destination           text NOT NULL,
    state                 text NOT NULL CHECK (state IN ('pending', 'confirmed', 'failed')),
    settled_amount_minor  bigint,
    settled_currency      text,
    created_at            timestamptz NOT NULL
);
