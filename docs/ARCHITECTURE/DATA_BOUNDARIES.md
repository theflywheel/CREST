---
title: Data boundaries
---

# Data boundaries

Three places data can live, and a rule for each.

## The public log

The DeDi node holds facts anyone may read: the instance's self-description, organisation faces, published definitions and their versions, recognised adapter versions, terms sets. Every record is tamper-evident, carries an inclusion proof, and is witnessed by the global node.

- **Never** a person's name, an identifier, a consent, a claim, or a window.
- The parties service refuses to publish a fact that holds personal data, whichever publisher is configured; the refusal is logged as a warning naming the record.
- A deployment without a node runs on a fallback that answers the same calls and says at boot that no transparency log is behind it.

## The private store

Postgres, one schema per member, holds everything about a person and every policy state: parties and their bindings, consents, grants, projects, holds, recoveries, units, claims, windows, credentials, instructions.

- A national identifier enters as a salted hash and a pairwise subject reference; the raw number is resolved at ingestion and discarded, never persisted, not even in fixtures.
- Private reads are scoped: a list of windows, instructions, claims or assessments names its project and is answered only to a caller holding the right function there. An unscoped read is refused, never answered with everybody's data.
- Consent artefacts (a voice recording) go to the object store; the row holds the key.
- Backups are quiesced and verified: [../DEPLOYMENT.md](../DEPLOYMENT.md) and `infra/acceptance/backup.sh`.

## The device

- The **worker's wallet** is encrypted on the device, exportable and restorable. It holds the worker's credentials; the service holds the record.
- The **field door's queue** is encrypted in IndexedDB; registrations made offline wait there and survive an app upgrade, and the plaintext they replaced is cleared only after the encrypted copy is durable.
- The **verify door** keeps nothing personal: an issuer key and a status list.

## Between services

Core and payments speak over `/internal/*` with signed service identity; the doors' nginx refuses those paths. Consequences cross the boundary through the outbox — a window opened, an exit recorded, a release owed — written in the same transaction as the record that caused them, and relayed until acknowledged.

