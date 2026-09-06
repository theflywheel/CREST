-- Review notification and acknowledgement evidence.
-- Provider acceptance is not worker reach. The token hash is the one-time
-- link credential; only an authenticated worker (or authorized agent) can
-- turn it into a reached review window.
ALTER TABLE windows ADD COLUMN review_started_at timestamptz;
ALTER TABLE windows ADD COLUMN review_token_hash text;
ALTER TABLE windows ADD COLUMN acknowledged_at timestamptz;
ALTER TABLE windows ADD COLUMN acknowledged_by text;
ALTER TABLE windows ADD COLUMN acknowledgement_reason text;
ALTER TABLE windows ADD COLUMN acknowledgement_evidence text;
ALTER TABLE windows ADD COLUMN support_owner text;
ALTER TABLE windows ADD COLUMN support_assigned_at timestamptz;
ALTER TABLE windows ADD COLUMN support_reason text;

CREATE UNIQUE INDEX windows_review_token ON windows (review_token_hash)
    WHERE review_token_hash IS NOT NULL;
