CREATE TABLE inbox_messages (
    message_digest      text PRIMARY KEY,
    provider_id         text NOT NULL UNIQUE,
    recipient           text NOT NULL,
    subject             text NOT NULL,
    body                text NOT NULL,
    acknowledgement_url text NOT NULL DEFAULT '',
    accepted_at         timestamptz NOT NULL
);

CREATE INDEX inbox_messages_recipient_accepted
    ON inbox_messages (recipient, accepted_at DESC);
