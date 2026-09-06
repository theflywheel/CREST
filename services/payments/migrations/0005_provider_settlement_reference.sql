-- Preserve the provider's terminal reference separately from the original
-- claim reference. This is additive so an applied provider migration remains
-- immutable and can still be verified by its recorded checksum.
ALTER TABLE payment_simulator_transfers
    ADD COLUMN settlement_reference text;
