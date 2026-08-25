# The demo layer — apps/web and the story seeder

CREST's design of record describes an infrastructure layer, and infrastructure
is invisible by construction. The demo layer exists to make it visible: one
static web app (`apps/web`) that renders the Actor Journeys against the real
services, and one seeder (`harness/story.go`) that gives those journeys a week
of coherent history to render. This document records the design decisions
behind both — what they are, what they refuse to be, and why.

Blueprint sections referenced: §10 payments boundary, §11 worker invariants,
§13 API surface, §15 journey scope map.

## What the demo layer is, in the layering

The layering test says: if two deployments could reasonably disagree about it
and both still be CREST, it is not infrastructure. The web app is the extreme
case — it is **L3 product**, one possible face over the L1 API, and every rule
below exists to keep it honest about that:

1. **It reads only what the services hold.** No client-side fixture data, no
   computed-in-the-browser judgements, no cached tiers. Every number on every
   screen is a `GET` against a `/v1` endpoint the e2e suite already exercises.
   If a screen is empty, the deployment's data is empty — the app never fills
   the silence.
2. **It authenticates through the real first-login path.** `loginAs` in
   `api.js` mints a token from the dev stack's mock OIDC issuer, then binds by
   token possession through `POST /v1/parties/{id}/identity-bindings` — the
   same self-proof path a production eSignet callback uses (#102). Replacing
   the mock with eSignet changes nothing after the token.
3. **It has no privileged door.** The app calls public `/v1` endpoints as the
   signed-in party. It cannot reach `/internal/*`; in the Railway deployment
   the nginx in front of it refuses those paths at the door (§16
   service-identity fence, DEPLOYMENT.md).
4. **What is not built is named, not faked.** Where the Actor Journeys promise
   a screen the backend cannot serve, the face says so and names the issue.
   The landing page lists the journeys deliberately undrawn (below). A greyed
   truth beats a painted mock, because a painted mock is how an untested path
   reaches a pilot looking finished.

## The design language, inherited from the Actor Journeys

The reference document (17 Aug) is not just a screen list; it carries a stance
about how a system whose records decide payment should talk to people. The app
adopts these rules from it:

- **Refusals are shown as refusals.** A field a verifier may not see renders
  as an explicit "withheld" value, never a silent omission (§11, J9).
- **Every held payment shows its reason and its owner.** The worker's money
  view and the project's payment view both render `held.{code, explanation,
  ownerPartyId}` — a missing payment with no explanation attached is the
  failure mode, and the empty-state text says so in words ("all four exits
  release one, a dispute included").
- **Closure is narrated.** After a confirm, dispute, or assisted confirmation,
  a "What just happened" block states the consequence — including, on a
  dispute, that the payment is released anyway. The state machine guarantees
  it; the screen says it, because a worker under the impression that disputing
  withholds money is a worker under duress (W4).
- **Unavailable is greyed, not hidden.** Options the deployment has not
  enabled appear disabled with a sentence, so the shape of the full journey
  stays visible.
- **Plain sentences over system vocabulary.** "My money", "Who checked me",
  "A spreadsheet arrived" — the navigation names what a person recognises,
  not what the service is called.

## Faces and their backing

Six faces, each built from its journey and backed only by endpoints that
exist. (The per-face table in `apps/web/README.md` is the maintained map;
this is the design summary.)

| Face | Journey | Design note |
|---|---|---|
| Worker | J7 (W-1) | The wallet, not a dashboard: record, what counts as done (the definition's worker face + rate), money with held reasons, credentials, who-checked-me trail, messages, consents |
| Supervisor (Attestor) | J8 (W-4) | Batch intake, unreached workers with assisted confirmation, the unclear queue |
| Registry Custodian | J6/J10 (W-2) | Resolve, duplicate holds (hold-never-merge, existence not content), unclear attribution, recoveries, overdue reviews |
| Project console | J11 (P-2, thin) | Funnel over real queues; payments grouped by held-reason with owners; trace-a-payment (the J10 support journey, read-only over the rail state); the definition with its tier map; sources. Metric contracts are #31 and the page says so |
| Verifier | J9 (V-1/V-2) | Verify with the checkable/trusting chain, resolve-a-person (#104), bounded batch checks (#107) |
| Registering Agent | J6 (W-2) | Field registration either pathway, voice consent, opening a recovery |

Deliberately undrawn, and named on the landing page: instance setup (J2),
organisation onboarding (J1), the 28-screen definition authoring console
(J4), rate/mechanism publishing (J5's wizards), and funder oversight (J11's
portfolio console). Each is L2 configuration or L3 product with no demo
backend behind it yet; drawing them would demonstrate nothing true.

## The story seeder

`harness/Seed()` builds the fixture **world** — parties, a definition,
grants — and the e2e scenarios each write their own history into it. A demo
stack seeded that way is a stage with no play: fifteen of the app's views were
honestly empty. The story seeder (`harness/story.go`, `SeedStory`) fixes that
by running one coherent week **through the real endpoints only** — it is a
scripted user, not a database load.

Design decisions:

- **Opt-in, never ambient.** `SEED_STORY=true` on `tools/seed`. The e2e
  scenarios assume a bare fixture world and must never find a pre-disputed
  claim in it.
- **Additive to the world, disjoint where it must be.** The story reuses the
  fixture parties (the app logs in as them) but story-only artefacts carry
  `story|`-prefixed subjects, and the duplicate-hold pair uses story-only
  people on a story-only phone number — a hold on a fixture worker's phone
  makes their identifier ambiguous and silently reroutes the e2e suite's
  batch rows to the unclear queue. The suite must pass on a storied stack,
  and does; that property is the seeder's real acceptance test.
- **Idempotent by observation, not by flag.** Re-running detects the story's
  registered source (`riverside-dhis2`) and declines with
  `ErrStoryAlreadySeeded`, then restores the story clock — because `Seed()`
  resets the driveable clock to epoch on every run, and a demo whose clock
  forgot the week just told is a demo with vanished open windows.
- **Every invariant demonstrable, on purpose.** The week is written so each
  absolute rule has a visible instance: all four T=7 exits including a
  dispute whose payment still releases; a zero-outcome claim held as
  `nothing_to_pay` with an owner; a duplicate that holds (409) and never
  merges; an open recovery; an overdue authorization that flags rather than
  lapses; a verification trail the worker can read. If a rule has no screen
  where a visitor can watch it hold, the demo is not demonstrating CREST.
- **It ends mid-story.** Three windows are left open so a visitor can
  confirm, dispute, or assist live, and watch the consequence — the demo is
  driveable, not a museum.

The seeder is also an instrument: forcing a scripted week through the public
API is how design finding #117 surfaced (the source heartbeat joins
`systemRef` against `adapter_ref`, so every source reads NEVER_SEEN). A demo
that can only be assembled by writing to the database directly would be a
finding about the API; this one found a real bug instead.

## How it is proven

Per `docs/test-manifest.md` (the maintained record): the story seeder and the
gap-closure views are **spike**-level — the full e2e suite passes on a
storied stack and a Playwright walk of all 26 persona/views renders real data
with zero console errors, both run on demand rather than in CI. The dev-login
path's equivalence to production login is likewise a spike (#102). Promoting
the storied-stack suite run into CI is open scope, not a claimed fact.
