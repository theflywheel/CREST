# Components: what we run, what we build

Two questions decide where every box goes:

1. **Does it already exist and carry an unforgeable property we cannot implement ourselves?** Then we run it, we do not rebuild it. Credential signing, transparency logs, identity assertion — these are trust primitives that took other people years and audits to get right.
2. **Is it work-aware?** Anything that knows what a *work event*, a *claim*, or a *definition* is, is ours. That knowledge is CREST; nothing upstream has it.

Everything below follows from those two questions.

---

## Run, don't build (docker-compose)

| Component | What it gives us | Why not ours |
|---|---|---|
| **Inji Certify** | OpenID4VCI issuance, credential signing, Bitstring Status List | Credential signing is exactly the property we must not hand-roll. A bug here is forged work history. |
| **Inji Verify** | OpenID4VP verification, offline QR | Same reason, other direction. Also the offline path is subtle and already solved. |
| **Inji Web wallet** | Browser wallet for testing | Inji Mobile is an installed app, not a compose service; the web wallet is what CI can drive. |
| **eSignet** (+ mock identity system) | OIDC broker to national ID; pairwise subject identifiers | Identity assertion is a national-authority function. We consume it; we never become it. |
| **DeDi-node** | Merkle transparency log, inclusion proofs, signed checkpoints, witness ring | Already ours as a project ([theflywheel/DeDi-node](https://github.com/theflywheel/DeDi-node), Go) with its own compose. Consume it as a service — do not vendor its source into this repo. |
| **PostgreSQL** | Durable state, schema-per-service | Obvious. One instance, one schema per service, **no cross-schema foreign keys** — the boundary is enforced by having no way to cheat. |
| **SeaweedFS** (S3 API) | Consent artefacts — including **voice consent recordings** | Assisted enrolment (#24) captures consent as audio for non-literate workers. That is a blob, and it needs object storage from day one, not "later". |
| **Mock SMS gateway** | Local channel testing | A real gateway is a paid external. The mock is ours (`tools/mocks/`), tiny, and lets the harness assert "the worker was notified" (W2) without a phone. |
| **Mock payment rail** | Local money-movement testing | Sandbox access takes weeks (#19) and lies about failure modes. The mock lets P2/P3 proceed and lets us *inject* failures a sandbox won't produce. |

**On object storage.** MinIO was the reflex choice and is no longer a good one — its community edition was stripped back in 2025 and the project's direction moved away from the self-hosting case. The default is now **SeaweedFS** (Apache-2.0, actively maintained, S3-compatible). [Garage](https://garagehq.deuxfleurs.fr/) is the other serious candidate and is arguably a better fit for geo-distributed in-country deployments, but it is AGPL-3.0, which some government hosting arrangements object to on reflex whether or not the objection is sound.

The architectural point is that **it should not matter much**. Consent artefacts go through an S3-compatible interface in `pkg/store`, so a deployment can point at SeaweedFS, Garage, AWS S3, or a national cloud's object store without touching CREST. Verify the license and maintenance position for whichever is chosen before the pilot — the choice above is a starting default, not a settled decision.

**No message broker, deliberately.** Kafka/NATS is the reflex here and it would be wrong at this size. We use a Postgres-backed transactional outbox: the state change and the intent to notify commit in the same transaction, which is precisely the property that stops a claim being confirmed while the payment instruction is lost. A broker gives throughput we don't need in exchange for a consistency problem we'd have to solve anyway.

**No Redis.** Idempotency keys and dedup live in Postgres with unique constraints. A cache that can silently disagree with the database is not what a payments path needs.

**Not in compose:** MOSIP IDA and real mobile-money rails are country/provider deployments we integrate with, never run. eSignet's mock identity system stands in locally.

---

## Build ourselves

### The six L1 services (Blueprint §13)

| Service | Owns |
|---|---|
| `registry` | Instances, organisations, terms, authorizations, worker records, the duplicate hold queue |
| `definitions` | Work definitions: author → ratify → ACTIVE, three-face rendering, DeDi publication |
| `evidence` | Adapter intake, canonical record validation, identity matching, unit + claim creation, the unclear queue |
| `confirmation` | The T=7 state machine; notification, confirm/dispute/auto-confirm, issuance calls to Certify |
| `verification` | Verifier passes, trust-chain walk, strength derivation, per-request disclosure consent, check audit trail |
| `payments` | Rate resolution, PaymentInstruction emission with idempotency, rail connectors, reconciliation |

### Supporting components, also ours

| Component | Why it exists |
|---|---|
| `notify` | One channel abstraction over SMS, USSD, WhatsApp and push. Not a seventh L1 API — an internal service, because every other service needs to reach a worker and none of them should know about a telco. |
| `adapters/csv` (then `dhis2`, `digit-hcm`) | Source-system integration. The claim under test is that these are **configuration, not code** (#30) — the CSV adapter is the reference implementation of that contract. |
| `crestctl` | One CLI for operators and the harness: seed a world, ratify a definition, advance the clock, drive a flow. The harness must drive the system through real interfaces, and this is one of them. |
| `pkg/*` | Shared Go: injectable clock, ID/DID generation, JSON Schema validation, DeDi/Certify/eSignet clients, audit, outbox, HTTP scaffolding. |
| `schemas/` | **The eleven primitives as JSON Schema — the source of truth.** Go structs and TypeScript types are generated from them, never hand-written in parallel. Two hand-maintained definitions of a primitive is two systems. |
| `harness/` | The E2E harness (#40) and the W1–W10 suite (#43). |
| `demos/` | Recorded journey walkthrough videos (J1–J12), regenerated by `tests/e2e-apps/journeys.mjs` against a story-seeded stack. Git-ignored — a demo is an artefact of the code, not a source of it. |
| `apps/worker`, `apps/enrolment`, `apps/console` | Worker PWA, field enrolment app, and one console with role-based faces (org / project / oversight) rather than three separate apps sharing 80% of their code. |

---

## Why Go, and where TypeScript lives

Go for services and the harness: it matches DeDi-node, produces small containers that make the harness cheap to run, and has the concurrency story the evidence pipeline needs. TypeScript only in `apps/`.

The two languages meet at `schemas/`. Both sides generate from the same JSON Schema, so a primitive can't drift between the service that writes it and the app that renders it.

## Repository shape

```
schemas/       JSON Schema — source of truth for both languages
services/      Six L1 services + notify (Go)
pkg/           Shared Go libraries
adapters/      Source-system adapters (Go)
cmd/crestctl/  Operator + harness CLI
apps/          The static-era doors (shrinking as they move, #153)
frontend/      pnpm workspace: rebuilt doors + @crest/ui, @crest/api (#153)
harness/       E2E scenarios, fixtures, W1–W10 suite
infra/compose/ Everything above, running
tools/         Mocks, codegen, structure lint
```

One Go module at the root. Boundaries are **enforced, not documented**:

- No service may import another service — they talk over HTTP, like they do in production.
- `pkg/` may not import `services/` — shared code cannot depend on a particular service.
- Only `pkg/store` may import a database driver.
- Only `pkg/clock` may call `time.Now()`. Everything else takes a clock, which is what makes a seven-day window testable in milliseconds.

These are `depguard` and `forbidigo` rules in `.golangci.yml`, checked in CI. A rule that lives only in a document is a rule that gets broken in month three.

## What runs where, at a glance

```
        ┌────────────── apps/ (TypeScript) ──────────────┐
        │  worker PWA    enrolment app    console        │
        └───────────────────────┬────────────────────────┘
                                │ HTTPS
        ┌───────────────────────┴────────────────────────┐
        │  registry  definitions  evidence  confirmation │  ← ours (Go)
        │  verification  payments  notify                │
        └───┬───────────┬──────────┬─────────┬───────────┘
            │           │          │         │
      ┌─────┴─────┐ ┌───┴─────┐ ┌──┴────┐ ┌───┴────────┐
      │ Postgres  │ │ S3-compat│ │ DeDi  │ │ Inji       │  ← run, not built
      │ (+outbox) │ │ (consent │ │ node  │ │ Certify /  │
      └───────────┘ │  blobs)  │ └───────┘ │ Verify     │
                    └──────────┘           └─────┬──────┘
                                               │
                                         ┌─────┴──────┐
                                         │  eSignet   │
                                         └────────────┘
```

## Honest gaps

- **Inji and eSignet image tags are unverified** until the P0 spike (#1, #3). They are pinned in `infra/compose/.env.example` with the version we *believe* is current; P0's job is to confirm that and record what differs in the findings memo (#4).
- **The compose file will not fully come up today.** Our services are skeletons — they build, serve health, and nothing more. That is the intended starting state, not a defect; the point of scaffolding first is that #20–#24 each begin with a place to write code and a way to run it.
- **`apps/` is structure only.** The frontends have no screens yet; the Actor Journeys HTML in `reference/` is their specification.
