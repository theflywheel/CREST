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

### The local-stack trap that has now cost two agents a morning

`make apps-up` re-runs the story seeder. Re-seeding a **live** volume rewrites
the programme organisation's identity binding to the fixture world's own
`subjectRef`, which this stack's mock issuer cannot derive — so every
console persona then fails to sign in with
`403 subject_not_enrolled`, and every screen behind a login looks broken.

The stack is fine; the binding is stale. Before believing a sign-in failure:

```sh
docker compose -f infra/compose/docker-compose.yml down -v && make apps-up
```

Do that once, then run `make e2e-apps` and `make fidelity` **without**
re-running `apps-up` in between. This is what issue
[#171](https://github.com/theflywheel/CREST/issues/171) actually was — it was
filed as a permissions bug and closed as a measurement error, and the
measurement error is reproducible enough to deserve writing down. The real fix
is for the seeder to leave an existing binding alone; until then, this
paragraph is the fix.

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

### A repeated failure is investigated, not re-run (#79)

[#79](https://github.com/theflywheel/CREST/issues/79) was filed after `make test-e2e` failed **every** scenario at `POST /v1/parties -> 500`, with no code change involved — a stack that had been started by hand against the deployed DeDi node, then run against by an invocation expecting the Postgres fallback. Nothing said so. Twenty identical scenario failures read exactly like a broken build, and re-running until it goes green launders a real hazard as noise.

**The convention:** if a scenario run fails and a second, unmodified run of the same command passes, that is not "flaky" — it is unproven. Write down what changed between the two runs (was the stack brought up freshly? was `DEDI_URL` set at some point in the session? does `docker volume ls` show a Postgres volume older than this run?) before dismissing it. The run header below and the preflight check exist so this investigation is usually one line instead of twenty minutes.

**What the preflight checks**, before any scenario assertion runs (`harness/preflight.go`, wired into `harness/scenarios`' shared `setup(t)`, run once per test binary):

- **Transparency substrate.** Every service's `GET /healthz` reports `"transparency": "dedi"` or `"postgres"` (`pkg/httpx`, reading `DEDI_URL`/`DEDI_PUBLISHER_KEY` the same way `infra/compose/docker-compose.yml` does). This must agree with what the harness process itself expects, from the same two variables in its own environment. This is the exact #79 hazard: a stack pointed at the real DeDi node, run against by an invocation expecting the local fallback (or the reverse).
- **Clock mode.** Every service's `GET /internal/clock` must report `"ticking": true` — a driveable `Offset` clock (`pkg/clock`). The harness moves time instead of sleeping; a service that cannot be driven fails every window-dependent scenario for a reason that has nothing to do with the scenario.
- **Clock agreement (#221).** Every process must think it is roughly the same time as every other: `/internal/clock`'s `now` where the service declares the seam, `/healthz`'s `time` where it does not, compared earliest against latest and failing past `harness.ClockSkewThreshold` (5m — the same as the payments application's `CLOCK_SKEW_ALERT` default, so the harness fails on the disagreement a deployment would warn about). Since #221 a confirmation window's opening instant is stamped by `evidence` and honoured by the payments application, which makes "what time do these two processes think it is" a question with a right answer. A skewed stack does not fail loudly: every window-crossing scenario simply asserts a number that is quietly wrong, and usually gets away with it. The threshold is generous because the statuses are gathered one service at a time over HTTP, and a suite that fails on the gathering is a suite people re-run.
- **Build revision.** If more than one distinct `revision` comes back across `GET /healthz` calls, that means one service was rebuilt and another was not — the same "which of these is stale" confusion one layer down.

On any mismatch it fails once, with a `StaleEnvironmentError` whose message always contains the literal phrase `stale environment` and names expected vs. actual for every mismatch found — see `harness/preflight_test.go` for the comparison function (`ComparePreflight`) exercised directly, with no Docker.

At the shell level, `make e2e-up` writes `.e2e/stack.fingerprint` — a hash of the compose config plus the environment variables that shape behaviour (transparency substrate, confirmation window, sweep cadence), and a start timestamp (`tools/e2e-fingerprint`). `make e2e-run` recomputes that hash from the *current* environment and fails, in the same `stale environment` shape, before the harness binary even starts, if they disagree or if no fingerprint exists at all. `make e2e-reset` (`docker compose down -v`, plus removing `.e2e/`) is the fix — it is the one target guaranteed to leave nothing behind, and it is what `make test-e2e` already does at both ends of its own run.

### Reading the run header

`make test-e2e` and `make e2e-run` both print a header before any scenario runs:

```
── e2e run header ──────────────────────────────────────────
transparency substrate : postgres
clock mode              : driveable (services take an Offset clock; the harness moves it — see docs/TESTING.md)
confirmation window     : 168h (default)
service revision        : 3f9a21c
compose project         : crest
db volume predates run  : no — created 4s ago, by this invocation
────────────────────────────────────────────────────────────
```

- **transparency substrate / clock mode / confirmation window** — what this invocation itself expects, from its own environment (the same thing the preflight checks the running services against).
- **service revision** — `git rev-parse --short HEAD` for this checkout, `+dirty` if there are uncommitted changes; a mismatch between this and what a service later reports on `/healthz` is exactly the "stale build" case the preflight names.
- **compose project** — always `crest` (fixed by `name: crest` in `infra/compose/docker-compose.yml`), so the header does not depend on where the command happens to be run from.
- **db volume predates run** — whether the compose project's Postgres volume (`crest_pgdata`) already existed before this invocation started it, by comparing the volume's `CreatedAt` against wall-clock now. `test-e2e`'s leading `down -v` means this should always read "no" there; on `e2e-run` a "yes" is the first thing worth reading before treating a failure as a defect.

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
