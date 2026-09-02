# apps/ — the journey apps

The Actor Journeys reference (`../docs/reference/`) encodes distinct frontend
products through its device frames; these are those products, built against
the live services. `apps/index.html` is the landing page of the door that
serves them; `make apps-up` brings everything up story-seeded on :59110.

**The doors are being rebuilt as desktop consoles** (responsive down to
mobile) in the `../frontend/` pnpm workspace — React + Vite + TypeScript,
`@crest/ui` carrying the design system, flows ported 1:1. The worker door
lives there now; the rows below move over one PR at a time, and this
directory shrinks to `web` (the untouchable PoC) plus whatever is not yet
ported. `make apps-build` builds the rebuilt doors and assembles the compose
docroot; `make apps-dev` runs their Vite dev servers.

| App | What it is | Reference journeys |
|---|---|---|
| `worker` *(moved to `frontend/apps/worker`)* | The worker's app: the wallet-not-a-dashboard — record, money **including why any of it is held**, credentials, who checked me, consents | J7 (W-1) |
| `enrolment` *(moved to `frontend/apps/field`)* | Field app: assisted registration, voice consent read aloud, duplicate holds, confirm-what-you-saw, close the roster. Offline-aware | J6 (W-2), J8 (W-4) |
| `console` *(moved to `frontend/apps/console`)* | One console, role-based views — project status/payments/trace, defining the work, payment set-up, organisation, instance, custodian queues, support, funder | J1–J5, J10, J11 |
| `verify` *(moved to `frontend/apps/verify`)* | Account-free checking: yes plus facts, refusals shown as refusals, bounded batches, the external-institution panel | J9 (V-1/V-2), P-10 |
| `shared` | The design system (DIGIT palette tokens lifted verbatim from the reference), the API client, the real first-login path, fixture ids | — |
| `web` | **The earlier single-page PoC, shared externally — do not touch.** Six faces in one page; superseded by the apps above but kept exactly as shared | — |

## Build discipline

- **No build step** was the rule for the first cut — plain ES modules served
  by nginx. The desktop-console rebuild (frontend/) retires it door by door:
  the rebuilt doors are Vite builds, and the day every door has moved this
  bullet goes with them.
- **Real endpoints only.** Nothing on any screen is fixture data held in the
  browser. Where the reference draws a screen no L1 endpoint serves, the
  screen exists and says so (`.open-note`, the reference's own "Illustrative,
  not a real API" chips) — never invented live-looking data.
- **Credential custody is Inji's job.** The worker app's wallet tab is a view;
  long-term custody is the deployed Inji wallet, and the un-wired
  Certify/OpenID4VCI import path is labeled as the gap.

## Why one console, not three

The consoles in the journeys share most of their surface. Role-based views
over one codebase, with the roles coming from the parties service — three
apps would fix every bug three times and drift until they disagree about what
a number means (the metric contracts, #31, exist to prevent exactly that).

## Proven how

`tests/e2e-apps/` — a Playwright walk of every route of every app against a
story-seeded stack: no JS exceptions, no API error banners, and the story's
data visible where the reference expects it. `make e2e-apps` locally;
`BASE_URL=https://… make e2e-apps` against a deployed door.

## Channel parity is a requirement, not a nice-to-have

Most workers in the first use case will not use a smartphone app. Every state
change with a deadline must reach a worker over SMS/USSD too (#29) — the
worker app states the USSD channel on its home screen, and any flow that only
works in the app is unfinished.
