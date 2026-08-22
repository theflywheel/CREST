# Testing strategy

The point of this document is that **nobody — human or agent — should have to reason from scratch about how a change is validated.** Every feature has a row in the [test manifest](test-manifest.md) saying how it is proven, and every layer below has a fixed command that runs without arguments.

## Why this exists before the code does

CREST's failure modes are not ordinary bugs. A miscomputed total is a defect; a payment that never posts with no reason attached is a worker not eating. The worker invariants **W1–W10** (Blueprint §11) are the things that must never break, and "never" is a claim only tests can carry. So the harness is specified now, and the invariants become executable at G3 (#33) — not written after the fact when the system is already too big to characterise.

## Four layers, four commands

| Layer | Command | Runs against | Owns |
|---|---|---|---|
| **Unit** | `make test-unit` | Pure functions, in-process | The strength function `f`, schema validation, state-machine transitions, rate arithmetic |
| **Contract** | `make test-contract` | Recorded fixtures, no network | Adapters producing canonical records; OpenAPI request/response shapes; credential JSON-LD shape |
| **Harness (E2E)** | `make test-e2e` | Real services, real HTTP | The whole spine: CSV → unit → claim → confirm → issue → verify |
| **Invariants** | `make test-invariants` | Real services | W1–W10 as executable acceptance tests |

`make test` runs unit + contract. `make test-all` runs everything, and is what CI runs on a PR.

**The rule that keeps this honest:** if a test needs a hand-written setup step described in prose, it is not done. The command must be enough.

## The harness

`make test-e2e` must do all of this itself, from a clean checkout, with no human in the loop:

1. Bring up Inji Certify, a DeDi node, Postgres, and the CREST services (docker-compose).
2. Wait for real readiness — poll health endpoints, never `sleep`.
3. Seed a known fixture world: one instance, one org, one project, the bednet definition (#18) ratified and ACTIVE, three workers at different identity-assurance levels.
4. Drive the system **through its real interfaces** — HTTP calls and CLI, never direct database writes. A test that reaches into the database is testing the database.
5. Assert on observable outcomes: a credential that verifies offline, a payment instruction with the right idempotency key, a dispute that still releases payment.
6. Tear down, and be re-runnable immediately without manual cleanup.

**Time is a first-class input.** The confirmation window is T=7 days; the harness must be able to advance the clock rather than wait. Services read time through an injectable clock, and the harness drives it. Any test that sleeps for real duration is a bug in the harness.

**Determinism.** Fixed seeds, fixed fixture IDs, no reliance on wall-clock dates or random ordering. A flaky harness gets ignored within a week, and then it is worse than nothing.

### Why it exists

Every manual test run costs a person twenty minutes and an agent a few thousand tokens, and the result is not recorded anywhere. The harness converts that into one command whose result is a diff. This is the single highest-leverage thing to build early, which is why it is a Phase 2 deliverable rather than a Phase 4 one.

## What gets a unit test

- **Anything with a truth table.** The strength function is the archetype: provenance facts + identity assurance → tier. It ships with test vectors (#15) covering every tier, every assurance level, and the retroactive-upgrade case.
- **Every state machine transition, including the ones that shouldn't happen.** The T=7 machine has four exits (confirm, dispute, auto-confirm, supervisor-assisted); all four release payment, and that is a test, not a comment.
- **Schema validation, both directions** — valid documents accepted, malformed ones rejected with a usable error. A schema that only gets tested with valid input is untested.
- **Money arithmetic**, with the awkward cases: partial periods, zero quantities, rate changes mid-period.

## What does not get a unit test

Mocked-out integrations asserting that a mock was called. That tests the mock. Integration behaviour belongs in the harness against real services — which is precisely why the harness has to be cheap to run.

## Fixtures

Fixtures live in `tests/fixtures/` and are **named after the situation, not the data**: `worker-with-no-phone.json`, `duplicate-hold-candidate.json`, `csv-with-unmatched-rows.csv`. A fixture called `test1.json` is a fixture nobody will reuse.

The canonical fixture world is defined once, in `tests/fixtures/world.yaml`, and both the harness and the invariant suite use it. Two divergent fixture worlds is the beginning of two divergent understandings of the system.

## PII in tests

**Never real personal data, in any fixture, ever — not even "anonymised" real data.** Generated names, generated identifiers, generated phone numbers in a reserved range. The identity tests specifically must use synthetic identifiers that no real identity system would issue.

This is not ceremony. `reference/` contains programme documents; test data drawn from a real programme is a data-protection incident waiting for a public repo.

## Deployment testing

A deploy is not done when it completes; it is done when it is verified. `make verify-deploy ENV=<env>` runs a smoke subset against a deployed environment: health of every service, one issuance, one verification, one payment instruction against the sandbox rail. It is read-mostly and safe to run against staging on every deploy.

Against production it runs only the read-only checks, because a smoke test that creates a real credential for a real worker is not a smoke test.

## In-toto (side quest, not yet)

Once the harness is real, there is a stronger claim available: **not just "the tests passed" but "this artefact was produced by this pipeline from these inputs, and here is the proof."** [in-toto](https://in-toto.io/) attestations would let CREST demonstrate supply-chain integrity for the thing that issues credentials — which matters more here than in ordinary software, because the whole system's value rests on the issuer being trustworthy.

The natural fit is signed attestations for each build and harness run, verified before deploy, eventually anchored in the DeDi log alongside the schema releases.

**Deliberately deferred** until the harness exists and P2 is passing. Attestations over a pipeline that isn't yet trustworthy would be proof of the wrong thing. Tracked separately so it does not get quietly forgotten.

## Keeping the manifest honest

The [test manifest](test-manifest.md) is the human-readable list of what exists and how it is proven. It is maintained by hand on purpose — the act of writing "how do I know this works?" is where the gap gets noticed.

A feature with no manifest row is unproven, whatever the coverage number says.
