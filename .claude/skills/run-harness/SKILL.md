---
name: run-harness
description: Run or extend the CREST end-to-end harness — bring up real services, drive the clock, assert on observable outcomes. Use when testing functionality end to end, debugging a cross-service failure, or adding a new E2E scenario.
---

# The E2E harness

One command, real services, no human in the loop. Its purpose is to stop costing a person twenty minutes and an agent thousands of tokens every time someone asks "does the flow still work?"

## Running it

```sh
make test-e2e                    # full spine
make test-e2e SCENARIO=<name>    # one scenario
make test-invariants             # W1–W10
make harness-up                  # leave services running for debugging
make harness-down                # tear down
```

If `make test-e2e` needs a manual step first, that is a defect in the harness — fix the harness, don't document the step.

## What it does

1. Brings up Inji Certify, a DeDi node, Postgres and the CREST services via docker-compose.
2. Polls health endpoints until genuinely ready. **Never `sleep`.**
3. Seeds the canonical fixture world (`tests/fixtures/world.yaml`): one instance, one org, one project, the bednet definition ratified and ACTIVE, three workers at different identity-assurance levels.
4. Drives everything through real interfaces — HTTP and CLI. Never direct database writes.
5. Asserts on observable outcomes: a credential that verifies offline, an instruction with the right idempotency key, a dispute that still released payment.
6. Tears down cleanly and is immediately re-runnable.

## Debugging a failure

```sh
make harness-up
make test-e2e SCENARIO=<failing>   # against the running stack
make harness-logs SERVICE=<name>
```

Inspect through the services' own APIs first. Reaching into Postgres tells you what the schema holds, not what the system does — and the gap between those two is usually the bug.

## Adding a scenario

Add it under `tests/e2e/`, named for the situation it covers, then:

1. **Add the manifest row** in `docs/test-manifest.md` — what feature, how proven, which layer. Same change, not later.
2. Reuse the canonical fixture world. If it genuinely lacks something, extend `world.yaml` rather than building a private world; two fixture worlds become two understandings of the system.
3. Drive time via the injectable clock. The confirmation window is seven days and the suite must still finish in seconds.
4. Assert the negative too. "The dispute was recorded" is half a test; "and the payment still released" is the half that matters.

## Time

Services read time through an injectable clock. The harness advances it:

```
advance_clock(days=7)   # auto-confirm fires
```

Any test that waits in real time will be deleted by whoever it blocks first, and rightly.

## When something looks flaky

Fix it or delete it. Never retry it. A suite that fails 5% of the time is ignored within a week, and then a real regression arrives dressed as noise.

Common real causes: readiness checked by sleep rather than health, fixture IDs generated per-run, tests sharing state through a database that isn't reset, or assertions on map ordering.
