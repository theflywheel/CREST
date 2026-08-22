# CREST

**Certified Record of Employment, Skilling and Training** — digital public infrastructure for verifiable work history for informal workers. First use case: trusted payments for community health workers.

## What this repo is

Design and implementation home for the **infrastructure layer (L1)** — the parts that must be identical in every CREST deployment. The configuration layer (L2) and products (L3) build on top of it.

The layering test: *if two deployments could reasonably disagree and both still be CREST, it is not infrastructure.*

## Design documents

| Document | What it covers |
|---|---|
| [Infrastructure Blueprint](docs/crest-infrastructure-blueprint.html) | **The design of record.** 11 primitives + profiles, registry/identity/credential/evidence/payments contracts, W1–W10 worker invariants, twelve-journey scope map, gap register, open decisions. Includes HLD, primitive-graph and information-flow diagrams |
| [Implementation Plan](docs/crest-implementation-plan.html) | Five phases (P0 substrate spike → P4 pilot readiness), gates G1–G3, workstreams, binding dependencies, risks |
| [CREST on Inji](docs/crest-inji-architecture.html) | How the Inji stack maps onto CREST's credential lifecycle |

Each is a self-contained HTML page — clone and open in a browser.

Full index with section map: [`docs/README.md`](docs/README.md). Source material sits in [`reference/`](reference/).

## The stack

- **[Inji](https://docs.inji.io/)** — credential lifecycle. Certify (OpenID4VCI issuance), Wallet (mobile + web), Verify (OpenID4VP, offline QR), PixelPass (printed cards).
- **[DeDi-node](https://github.com/theflywheel/DeDi-node)** — Merkle transparency-log directory for public registry facts (orgs, definitions, authorizations, schema releases).
- **eSignet / MOSIP IDA / OIDC / mobile OTP** — identity anchors. CREST stores a pairwise subject reference and a salted ID hash, never a raw national ID or biometric.
- **CREST services** — six L1 APIs: `registry`, `definitions`, `evidence`, `confirmation`, `verification`, `payments`.

## Core model in one paragraph

A **work event** is the atomic unit. A `Unit` exists independent of who performed it; a `Claim` links a `Party` to a `Unit`, so a disputed claim never destroys the underlying record. Work `Definition`s are three-faced (worker / platform / verifier), immutable once ACTIVE, and versioned. Credentials store **provenance facts** (source class, capture method, adapter ref) — trust strength is *derived at query time*, never stored. Confirmation runs a T=7 day confirm / dispute / auto-confirm window; every route releases payment.

## Status

Design complete, pre-implementation. **Phase 0 (substrate spike) is the next work** — nothing in it blocks on a decision, so it starts immediately.

Work is tracked as six phase epics with thirty-three sub-issues across five milestones; see [docs/TRACKING.md](docs/TRACKING.md) for the conventions, the gates and the blocking dependencies.

| Phase | Epic | Due |
|---|---|---|
| P0 · Substrate spike | [#34](../../issues/34) | 2026-09-04 |
| G1 · The six decisions | [#35](../../issues/35) | week 4 |
| P1 · Specification bundle | [#36](../../issues/36) | 2026-09-25 |
| P2 · Core loop slice | [#37](../../issues/37) | 2026-10-30 |
| P3 · Payments + verification | [#38](../../issues/38) | 2026-12-11 |
| P4 · Inclusion, oversight, federation | [#39](../../issues/39) | 2027-02-05 |

Three things sit on the critical path this week: book the G1 session for week 4, start P0, and open the mobile-money sandbox conversation ([#19](../../issues/19)) — that lead time is external and lands on P3.
