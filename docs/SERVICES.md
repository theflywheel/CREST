# The backend services

The roster (#148, reshaped by #150): every deployable CREST runs, what layer
it lives in, and how to reach it. The design behind the boundaries is
Blueprint §13; what we run versus what we build is COMPONENTS.md.

Since #150 there are **two deployables**. The four infrastructure boundaries
run as member packages of one `core` service — each keeping its own Postgres
schema, migrations, outbox and route family — and the payments application
stays its own service because it is the application layer (#127) and the
fleet should keep saying so. Neither carries a public domain: the doors'
nginx proxies `/api/crest-<name>/…` over private networking, the four old
member names alias onto `crest-core`, and the §16 fence refuses
`/internal/*` at the door.

## The two services

| Service | Deployed as | Layer | Local port | Members / what it owns |
|---|---|---|---|---|
| `services/core` | `crest-core` | infrastructure | 59000 | **parties** (parties, identity bindings — pairwise ref + salted hash, never a raw ID — consent, org registries on DeDi) · **definitions** (unit/claim definitions, lifecycle, grants) · **evidence** (ingestion, canonical evidence, source heartbeats, the outbox that opens windows) · **verification** (trust strength, passes, and the credential substrate since #137: issuer key in Vault (#130), issuance, status list, revocation, the printed card) |
| `services/payments` | `crest-payments` | **application** | 59006 | The payments application on the substrate (#127/#129): confirmation windows, all four exits, the sweep, contests, payment instructions, holds with owned reasons, reconciliation |

Names that look like services but aren't: `crest-registry`, `crest-definitions`,
`crest-evidence` and `crest-verification` are aliases for `crest-core`, and
`crest-confirmation` for `crest-payments` — kept so links already in the wild
proxy instead of 404ing. `crest-seed` is a one-shot job, not a server.
`notify` is **gone** (#150): notifications are dropped entirely for now, a
recorded gap (Blueprint §16) — a worker learns about a window or a held
payment only by opening the app.

## The mocks

Demo and local stand-ins, never part of the product:

| Mock | Deployed as | Local port | Stands in for | Retires when |
|---|---|---|---|---|
| `mock-oidc` | `crest-mock-oidc` | 59103 | eSignet login | #130 promotes eSignet to the real login |
| `mock-rail` | `crest-mock-rail` | 59102 | The payment rail | #26 lands a real rail connector |

## The substrate

Run, not built: Postgres (one instance, schema per member), Redis, the object
store, Vault (the issuance seed's custody, #130), the DeDi node, and — behind
the compose `substrate` profile, being promoted from spike by #130 — eSignet,
Inji Certify, Inji Verify and Inji Web. COMPONENTS.md carries the full
run-vs-build reasoning. RBAC, when it is needed, is OpenFGA (Blueprint §14).
