---
title: The API surface
---

# The API surface

Five boundaries, named by what they answer, all under `/v1`, reached through a door's nginx as `/api/crest-<service>/…`. The OpenAPI contracts are in [`schemas/openapi/`](https://github.com/theflywheel/CREST/tree/main/schemas/openapi); the JSON Schemas in [`schemas/`](https://github.com/theflywheel/CREST/tree/main/schemas) are the source of truth for every type.

## Conventions every call follows

| Convention | What it means |
|---|---|
| **Bearer token** | Every call carries the caller's OIDC token; the services resolve it to a party. Anonymous reads exist only for public facts (the issuer, the status list, the adapter catalogue) and for verification itself. |
| **Idempotency-Key** | Every mutation carries one. The same key replays the same act; a different body under the same key is refused. |
| **Act for a party** | An agent acting in a worker's name sends `X-CREST-On-Behalf-Of` and must hold `act-for-party` in that context. The record names both. |
| **Scoped reads** | A list of anything private (windows, instructions, claims, sources, assessments) names its `contextId` and is answered only to a caller holding the right function there. An unscoped read is refused, never answered with everybody's data. |
| **Errors name their cause** | `{code, message}` in words a caller can act on: `wider_than_terms`, `source_not_registered`, `not_rate_owner`, `mechanism_not_live`. |
| **Reference numbering** | Tiers are 1 (strongest) to 3; a definition written under the older numbering declares `tierSemantics` and is translated. |

## The five boundaries

| Boundary | A product calls it to | Routes |
|---|---|---|
| **parties** | Register organisations and people; bind identities; capture consent; grant, read and check authorizations (`/authorizations/permits`); create projects, invitations, roles; resolve an identifier; work holds and recoveries | `/v1/parties`, `/v1/organisations`, `/v1/projects`, `/v1/invitations`, `/v1/terms`, `/v1/authorizations`, `/v1/consents`, `/v1/resolve`, `/v1/holds`, `/v1/recoveries`, `/v1/instance` |
| **definitions** | Draft, dry-run, submit, ratify and activate definitions; attach linked records; read the faces a definition publishes | `/v1/definition-drafts`, `/v1/definitions`, `/v1/adaptors` |
| **evidence** | Register sources; submit batches; read units, claims and receipts; resolve the unclear queue | `/v1/sources`, `/v1/batches`, `/v1/units`, `/v1/claims`, `/v1/unclear`, `/v1/adapters` |
| **verification** | Read a person's credentials as them; verify one; assess a source per project; run share requests; read the presentation trail | `/v1/credentials`, `/v1/verify`, `/v1/status-list`, `/v1/issuer`, `/v1/source-assessments`, `/v1/presentations`, `/v1/presentation-requests` |
| **attestation** | Read windows; drive the four exits; acknowledge; handle contests; sweep | `/v1/windows`, `/v1/claims/{id}/confirm`, `…/dispute`, `…/assist`, `/v1/contests`, `/v1/unreached`, `/v1/unreleased`, `/v1/sweep` |

The payments application is a client of these five, not a sixth. Its own routes (`/v1/definitions/{id}/rates`, `/v1/mechanisms`, `/v1/instructions`, `/v1/reconciliation`) are the shape a product's surface takes: named by what it answers, scoped to a project, refusing readably.

## Consequences cross by the outbox

A product does not read another service's tables. It subscribes to what the members emit:

| Emitted by | Topic | Carries |
|---|---|---|
| evidence | a window to open | claim, unit, party, context, definition version |
| attestation | an exit | claim, route, authoritative time |
| attestation | a release owed | claim, party, context |
| parties | a notification moment | the facts, never the wording |

Each is written in the same transaction as the record that caused it and relayed until acknowledged; a subscriber that is down when it happens receives it when it returns. The relay is `pkg/store`'s; a product registers a deliverer the way `services/payments` does.

## Service-to-service

`/internal/*` routes answer services with signed service identity (`pkg/serviceauth`) and are refused at every door. A product that needs one is a product that should ask for a public route instead; the internal surface exists for the members of core and the payments application, and every addition is a design decision.

