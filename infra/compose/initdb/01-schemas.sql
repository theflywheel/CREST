-- One schema per service, no cross-schema foreign keys.
--
-- The boundary between services is enforced by making it impossible to cheat:
-- if payments cannot reference a registry table, the coupling it would have
-- created never exists. Services join over HTTP or not at all.

CREATE SCHEMA IF NOT EXISTS registry;
CREATE SCHEMA IF NOT EXISTS definitions;
CREATE SCHEMA IF NOT EXISTS evidence;
CREATE SCHEMA IF NOT EXISTS confirmation;
CREATE SCHEMA IF NOT EXISTS verification;
CREATE SCHEMA IF NOT EXISTS payments;
CREATE SCHEMA IF NOT EXISTS notify;

-- The outbox lives beside each service's own tables so a state change and the
-- intent to publish it commit in one transaction. That is the property that
-- stops a claim being confirmed while its payment instruction is lost.
