DO $$
BEGIN
    IF to_regclass('payments.windows') IS NOT NULL THEN
        LOCK TABLE payments.windows IN ACCESS EXCLUSIVE MODE;
        INSERT INTO windows
        SELECT (jsonb_populate_record(NULL::windows, to_jsonb(w))).*
        FROM payments.windows w
        ON CONFLICT (claim_id) DO NOTHING;
        INSERT INTO contests
        SELECT (jsonb_populate_record(NULL::contests, to_jsonb(c))).*
        FROM payments.contests c
        ON CONFLICT (id) DO NOTHING;
        INSERT INTO outbox(topic, payload, attempts, created_at, next_attempt_at, last_error)
        SELECT topic,payload,attempts,created_at,next_attempt_at,last_error
        FROM payments.outbox
        WHERE delivered_at IS NULL AND topic IN ('notify.claim','payment.release');
        UPDATE payments.outbox SET delivered_at=now(),last_error='Transferred to core attestation'
        WHERE delivered_at IS NULL AND topic IN ('notify.claim','payment.release');
    END IF;
END $$;
