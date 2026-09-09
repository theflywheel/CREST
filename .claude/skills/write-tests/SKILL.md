---
name: write-tests
description: Write tests for CREST the way this project tests — pick the right layer, name fixtures by situation, configure short durations and poll instead of sleeping, and update the test manifest. Use whenever adding or changing tests, or when asked how something should be validated.
---

# Writing tests for CREST

Full rationale in `docs/TESTING.md`. This is the operating procedure.

## Pick the layer first

| The thing under test | Layer | Command |
|---|---|---|
| A function with a truth table | Unit | `make test-unit` |
| A document shape, an adapter's output, an API contract | Contract | `make test-contract` |
| Several services agreeing | E2E harness | `make test-e2e` |
| A promise to the worker (W1–W10) | Invariants | `make test-invariants` |

Choosing wrong is the common failure. A test that mocks three services to assert a fourth was called belongs in the harness, or nowhere.

## Rules that are not negotiable

**Never sleep a fixed guess and then assert.** The confirmation window is seven days in the CHW programme, and there is no clock to drive through it (ruled 2026-09-09): services read `time.Now().UTC()`, and every time-bound behaviour is a configured duration. So the stack is brought up with the duration set to *seconds* and the test waits real time for the real outcome — polling with a bounded deadline (`harness.WaitFor`, `eventually` in the scenarios), never sleeping a guess.

A unit test that used a fake clock passes `time.Time` values in, or configures a small real duration and waits it out. A `time.Sleep` is only ever the scenario's own input — a gap you are deliberately creating — and never the wait for a result.

Deadlines are derived from the cadence the stack is running at (`harness/durations.go`, `harness.Patience`), and are generous: a tight multiple of the interval is a test that fails on a loaded runner and gets re-run rather than read.

**Never write to the database directly.** Drive the system through its real interfaces — HTTP, CLI. A test that seeds state by INSERT is testing your understanding of the schema, not the system.

**Never use real personal data.** Not real names, not real phone numbers, not "anonymised" real data from a programme. Generated values only, phone numbers from a reserved range, synthetic identifiers no real identity system would issue. `reference/` holds real programme documents; nothing from them becomes a fixture.

**Test the negative.** A schema tested only with valid documents is untested. A state machine tested only on its happy path is untested. The disallowed transition is the test that matters — it is the one that stops a worker's money moving to the wrong place.

## Fixtures

Live in `tests/fixtures/`, named for the **situation**: `worker-with-no-phone.json`, `duplicate-hold-candidate.json`, `csv-with-unmatched-rows.csv`. Never `test1.json`.

One canonical fixture world, `tests/fixtures/world.yaml`, shared by the harness and the invariant suite. Two fixture worlds become two understandings of the system, and then the disagreement surfaces in production.

## Determinism

Fixed seeds. Fixed fixture IDs. No wall-clock dates, no reliance on map ordering, no "usually passes". A flaky suite is ignored within a week and is then worse than no suite, because it launders failure as noise.

If a test is flaky, fix it or delete it. Do not retry it.

## The manifest is part of the change

Every feature has a row in `docs/test-manifest.md` stating **how it is proven**. In the same commit, not later.

- New feature → new row, status `planned` → `covered` when the checks pass.
- Feature exists with no validation → status `unproven`, which is a bug, not a state.
- Closing an issue → its rows are `covered`, or the issue explains why not.

The row is written in the form "how would I know if this broke?" Writing that sentence is where the missing test gets noticed — which is the entire reason the file is maintained by hand.

## Invariant tests are different

W1–W10 tests assert things that must be true **always**, including for absence. W9 ("identity is never over-collected") cannot be proven by a happy-path test; it is proven by scanning the actual datastore for anything resembling a raw identifier and asserting nothing is found.

When you touch anything in evidence, confirmation, payments or verification, ask which invariant could break, and add the assertion that would catch it.
