-- Delivered notification payloads must not retain bearer acknowledgement
-- tokens. Pending/claimed rows keep their token until the provider accepts the
-- message, so a retry can still construct the acknowledgement link.
UPDATE outbox
SET payload = payload - 'ackToken'
WHERE topic = 'notify.claim' AND delivered_at IS NOT NULL
  AND payload ? 'ackToken';
