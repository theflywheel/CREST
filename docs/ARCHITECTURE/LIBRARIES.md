---
title: Shared libraries
---

# Shared libraries (`pkg/`)

| Package | Responsibility | The rule it serves |
|---|---|---|
| `schema` | Go types generated from `schemas/`; the JSON Schemas are the source of truth (`make generate`, checked in CI) | One definition of a primitive, not two |
| `strength` | Trust tier derivation: provenance facts and a definition's tier map in, tier and reasons out, at query time | Trust strength is derived, never stored |
| `identity` | Token verification against one or many issuers; the binder from subject to party; the act-for-party rule and the assisting header | Nobody acts in another's name without a grant |
| `pii` | The salted hash a national identifier becomes before anything persists it | Never persist a raw national ID |
| `idempotency` | Durable idempotency keys for every mutation | A retry after a crash is the same act, not a second one |
| `store` | Postgres access, migrations per member, the outbox and its relay | A record and its consequence are written in one transaction |
| `credential` | Signing and verifying the work-event credential | A credential verifies anywhere |
| `dedi` | The registry substrate's four operations, and the honest fallback when no node is configured | Public facts are witnessed; the absence of a log is announced |
| `serviceauth` | Signed service identity between core and payments | `/internal/*` answers services, never doors |
| `clock` | A driveable clock for the harness; a live one everywhere else | A week can be tested in seconds; production never lies about time |
| `notify` | Delivery through a configured sender | The moment and the facts are infrastructure; the wording is configuration |
| `httpx` | Error shapes, JSON handling, the build revision | An error names its cause in words a caller can act on |
| `esignet` | The eSignet client for the doors' login | One integration per provider class |

`adapters/` sits beside `pkg/`: the `Adapter` interface, the registry, the built-in list and the conformance suite. It is documented in the [PLUGINS section](../PLUGINS/EVIDENCE_ADAPTERS.md).
