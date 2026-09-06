CREATE TABLE consent_upload_intents (
    operation_hash text PRIMARY KEY,
    object_key text NOT NULL UNIQUE,
    updated_at timestamptz NOT NULL DEFAULT now()
);
