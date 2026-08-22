# P0 findings memo (#4)

Where the substrates differ from what the design assumed. **In progress** — #2 has landed, #1 and #3 are partial. Phase 1 schema decisions stay open until this memo closes.

Each entry names the blueprint section it touches and the correction it forces. Entries that only cost us operational work are marked as such; entries that change the design are not.

Last updated 2026-08-22.

---

## Registry — DeDi (#2) · Blueprint §3 · **spike passing**

Full write-up: [spikes/dedi.md](spikes/dedi.md).

The substrate does what §3 assumes. Publish, inclusion proof, independent validation, versioning and compare-and-swap writes all work, and a proof issued against v1 still validates after v2 is published — which is the property credentials depend on.

| # | Finding | Effect |
|---|---|---|
| R1 | No published DeDi image; the repo is private and carries its own Dockerfile | Operational. Node is built from a checkout (`make dedi-image`). |
| R2 | DeDi expects its own Postgres | Operational, and correct: the log's checkpoints must not be reachable by a CREST migration. |
| R3 | **An unrecognised query parameter is ignored, not rejected.** `?versionId=` returns the *latest* version with a valid proof attached; the parameter is `version_id` | **Design.** See below. |
| R4 | `If-Match` wants `<digest>-<state>`, but lookup returns those as two fields with no combined tag | Minor. Clients re-derive a format the server should own. |
| R5 | DeDi-node needs Go 1.25; CREST is on 1.24 | None. Separate modules, HTTP between them. Forecloses vendoring, which is right anyway. |

**R3 is the finding with teeth.** A verifier that misspells the version pin silently resolves a *different* definition and receives a perfectly valid proof for it. Nothing in the response says "you did not get what you asked for". Applied to CREST, that is a verifier checking a credential against the wrong version of the work definition and having no way to notice.

This does not contradict §3, it adds a requirement to it: **CREST's registry interface must reject unknown query parameters rather than answer a different question.** Recorded against #20, where the DeDi abstraction is built.

**Not covered:** consistency proofs and witnessing. Inclusion proves a record is in *a* log; consistency proves the log was never rewritten, and that is the property an independent verifier actually leans on. Deferred to #20 and named here so it is not mistaken for done.

---

## Credential substrate — Inji (#1) · Blueprint §5 · **partial**

The image audit is complete and it changed the picture. The issuance demo is not done.

| # | Finding | Effect |
|---|---|---|
| C1 | **`mosipid/inji-verify` is not a component.** Our pinned tag `0.11.0` does not exist — that repository stops at 0.10.0, amd64-only, last pushed October 2024. The live components are `inji-verify-service` and `inji-verify-ui`, two images on their own version line (0.16.0) | Corrected in compose. Had we not checked, the first attempt to bring up the stack would have failed on a component nobody could find. |
| C2 | `inji-verify-ui` publishes **amd64 only** | Runs under emulation on Apple Silicon. Fine for a UI, not fine for anything we measure. Pinned explicitly so it is a stated choice. |
| C3 | Every other tag we had guessed was stale but real: certify 0.11.0 → **0.13.1**, esignet 1.5.0 → **1.8.0**, mock-identity 0.10.0 → **0.13.0**. All publish arm64 | Corrected in compose; all four pulled and verified. |
| C4 | Certify's `local` profile is genuinely self-contained: a PKCS12 softHSM keystore, `spring.cache.type=simple`, no Spring Cloud Config server. **Redis is present on the classpath but commented out of the configuration** — it is not required | Good news, recorded because "Spring app, therefore Redis" was the assumption worth checking. It does not disturb "no Redis" in COMPONENTS.md, which is about CREST's own services. |
| C5 | Certify needs database `inji_certify`, schema `certify`, and **`ddl-auto=none`** — the DDL is applied from the upstream repo's `db_scripts`, not created on boot | Operational, and it must be wired into compose init before #1 can proceed. |
| C6 | Certify's local profile points its authorization server at eSignet on `localhost:8088` | Certify and eSignet come up together or not at all. Affects how #1 and #3 are sequenced: they are one bring-up, not two. |

**Still open on #1:** issuing a hand-authored `WorkEventCredential` over OpenID4VCI, holding it in a wallet, rendering a printed card via PixelPass, and verifying that card **fully offline**. The offline verification is the part that matters — it is W6 — and it needs a real device with its radios off. A container asserting it has no network is weaker evidence, and the honest place to prove it is the field simulation the test manifest already schedules.

---

## Identity — eSignet (#3) · Blueprint §4 · **not started**

The image is pulled and the tag is confirmed (C3), and C6 means it will be brought up alongside Certify. Nothing has been verified about the thing #3 actually asks:

- whether the subject identifier is **pairwise per relying party** and stable across sessions — §4.1 assumes both;
- which claims come back, and which of them CREST must never persist (W9);
- what production access requires and how long it takes.

None of that can be answered against a local mock: a mock identity system will return whatever its fixtures say, including a pairwise subject, whether or not the real deployment does. **#3 needs a real sandbox and a relying-party registration**, and that is a conversation with a lead time, not a task. Like the rail sandbox (#19), it should be opened now rather than when the phase reaches it.

---

## What this memo already changes

- `infra/compose/docker-compose.yml` and `.env.example` — corrected images and topology (C1, C2, C3), DeDi's own database and key material (R1, R2).
- `Makefile` — `dedi-image`, `dedi-keys`, `spike-dedi`.
- A requirement on #20: reject unknown query parameters (R3).

## What it does not change

No primitive, credential shape or evidence-contract decision moves on the strength of what is here. §3 stands as written; §5 and §4 have not yet been tested where they make their riskiest claims — offline verification and pairwise identity respectively. **This memo cannot close, and Phase 1 schemas cannot freeze, on an image audit.**
