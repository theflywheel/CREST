# The backend services

The list that was missing from this index (#148): every deployable CREST runs,
what layer it lives in, and how to reach it. The design behind the boundaries
is Blueprint §13; what we run versus what we build is COMPONENTS.md. This page
is the roster.

Every service is one Go binary built from `services/<dir>`, owns one Postgres
schema, and answers on `:8080` in its container. None carries a public domain:
the doors' nginx proxies `/api/crest-<deployed name>/…` over Railway private
networking, and the §16 fence refuses `/internal/*` at the door.

## The six services

| Service | Deployed as | Layer | Schema | Local port | What it owns |
|---|---|---|---|---|---|
| `services/parties` | `crest-registry` | infrastructure | `parties` | 59001 | Parties, identity bindings (pairwise ref + salted hash, never a raw ID), consent records, organisation registries on DeDi |
| `services/definitions` | `crest-definitions` | infrastructure | `definitions` | 59002 | Unit and claim definitions, their lifecycle, authority grants |
| `services/evidence` | `crest-evidence` | infrastructure | `evidence` | 59003 | CSV/adapter ingestion, canonical evidence records, source heartbeats, the outbox that opens windows |
| `services/verification` | `crest-verification` | infrastructure | `verification` | 59005 | Trust-strength derivation, verification passes, and — since #137 — the credential substrate: issuer key (in Vault, #130), issuance, status list, revocation, the printed card |
| `services/payments` | `crest-payments` | **application** | `payments` | 59006 | The payments application on the substrate (#127/#129): confirmation windows, all four exits, the sweep, contests, payment instructions, holds with owned reasons, reconciliation |
| `services/notify` | `crest-notify` | infrastructure | `notify` | 59007 | Notification templates and SMS delivery |

Two names that look like services but aren't: `crest-confirmation` is a
retired deployable — the confirmation window lives inside payments since #129,
and the doors keep `/api/crest-confirmation/` as an alias so links already in
the wild proxy there instead of 404ing. `crest-seed` is a one-shot job that
seeds the demo story, not a server.

## The mocks

Demo and local stand-ins, `infra/compose/mocks/`, never part of the product:

| Mock | Deployed as | Local port | Stands in for | Retires when |
|---|---|---|---|---|
| `mock-oidc` | `crest-mock-oidc` | 59103 | eSignet login | #130 promotes eSignet to the real login |
| `mock-rail` | `crest-mock-rail` | 59102 | The payment rail | #26 lands a real rail connector |
| `mock-sms` | `crest-mock-sms` | 59101 | An SMS gateway | Local runs keep it indefinitely |

## The substrate

Run, not built: Postgres (one instance, schema per service), Redis, the object
store, Vault (the issuance seed's custody, #130), the DeDi node, and — behind
the compose `substrate` profile, being promoted from spike by #130 — eSignet,
Inji Certify, Inji Verify and Inji Web. COMPONENTS.md carries the full
run-vs-build reasoning.
