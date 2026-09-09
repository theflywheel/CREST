---
title: Local development
---

# Local development

```sh
make apps-up      # the stack, the four doors, the fixture world and a story week
make e2e-apps     # walk every door route with Playwright
make fidelity     # hold every built screen to the design reference
make test-e2e     # the spine scenarios on real services
make test         # unit tests
```

## The seeded world

`make apps-up` seeds two things through the public API only, never the database:

- **The fixture world** (`tests/fixtures/world.yaml`): parties, a definition, terms and grants; the community health profile.
- **The story week** (`harness/story.go`): consents, sources, batches, every kind of window exit, a held payment, a duplicate hold, an open recovery, an overdue grant. Written so every rule has a screen where you can watch it hold, and ending mid-story so three windows are open for you to confirm, dispute or assist live.

Every person in it signs in through the mock issuer locally and through eSignet on the fleet with PIN `112233`. ../DEMO.md (`docs/DEMO.md` in the repository) records what the story shows and refuses to fake.

## Things that bite

| Symptom | Cause | Fix |
|---|---|---|
| Every grant reads as inactive; `403 custodian_not_assigned` everywhere | The fixture world was seeded on its own fixed dates, which are not around today | Seed with `SEED_LIVE_CLOCK=true` (the default whenever `SEED_STORY` is set), which slides the fixture's dates so the programme's week lands on about now. There is no clock to set: every service reads real time |
| `payment subscriber disabled; release remains unreleased` | Core was started without `PAYMENT_SUBSCRIBER_ENABLED=true` | `make e2e-up` sets it; a hand restart must too |
| Seed refuses the instance operator | `CREST_OPERATOR_PARTY_ID` in `infra/compose/.env` points at another party | Override it for the fixture world: `CREST_OPERATOR_PARTY_ID=did:crest:party:01JCREST000000000000000RGN` |
| Core cannot resolve `dedi` | `.env` names a DeDi node the compose profile did not start | Start it first, or unset `DEDI_URL` to run the announced fallback |
| A second `make apps-up` over a seeded database fails on a linked record | Grants and linked records are immutable; the re-seed is not idempotent past that point | `docker compose down -v` and seed fresh, as CI does |

## The gates a change passes

1. The pre-commit hooks: gofmt, structure, lint, unit tests, no raw identifiers, and the unproven-work reminder.
2. CI: Go and race tests; the spine on real services; the journey walk and the fidelity gate; compose and docs checks.
3. Review: an automated reviewer reads every push and blocks on findings; a human merges.
4. Bookkeeping in the same change: the issue's "done when", the manifest row, the PR body naming the rule a change could break and how it was proven.

../TESTING.md (`docs/TESTING.md` in the repository) explains the layers; ../TRACKING.md (`docs/TRACKING.md` in the repository) the board; ../DEPLOYMENT.md (`docs/DEPLOYMENT.md` in the repository) how a merge reaches the fleet.
