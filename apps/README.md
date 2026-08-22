# apps/ — the TypeScript frontends

Structure only for now. The screens are specified by the Actor Journeys HTML in
[`../reference/`](../reference/), and mapped to scope in Blueprint §15.

| App | What it is | Arrives with |
|---|---|---|
| `worker` | Worker PWA: wallet, the confirmation window, earnings **including why a payment is held** | #28 |
| `enrolment` | Field enrolment: assisted registration, voice consent capture, printed card | #24 |
| `console` | One app, three role-based faces — organisation, project, oversight | #31 and the console journeys |

## Why one console, not three

The three consoles in the journeys share most of their surface: lists of workers,
claims, definitions, and metrics. Building three apps means fixing every bug three
times and letting the three drift apart until they disagree about what a number
means — which is exactly what the metric contracts (#31) exist to prevent.

Role-based faces over one codebase, with the roles coming from `registry`.

## Types come from schemas/

TypeScript types are **generated** from `../schemas/`, the same JSON Schema the Go
services generate structs from. Never hand-write a type that mirrors a primitive:
the moment there are two definitions, they start disagreeing, and the disagreement
surfaces as a worker seeing the wrong thing.

## Channel parity is a requirement, not a nice-to-have

Most workers in the first use case will not use a smartphone app. Every state
change with a deadline must reach a worker over SMS/USSD too (#29) — so the app is
one channel among several, and any flow that only works in the PWA is unfinished.
