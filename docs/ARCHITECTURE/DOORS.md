---
title: The doors
---

# The doors

Four product faces, each a React application served as a static site with the design system shared from `frontend/packages/ui`. Every door signs in through the deployment's identity provider; what a person sees is derived from the grants they hold, never from a chooser.

| Door | Path | Who | What it does |
|---|---|---|---|
| **Console** | `/console/` | Instance operator, organisation admin, project configurator, definition author and approver, rate owner, mechanism owner, registry custodian, support agent, funding viewer | Stands the instance up, publishes terms, admits organisations, sets projects up and invites, writes and ratifies definitions, publishes rates, activates mechanisms, works the custodian's queues, reads the dashboards. |
| **Enrolment** | `/enrolment/` | Registering agent, supervisor | Registers workers on either pathway with consent captured at enrolment, keeps an encrypted offline queue that survives an upgrade, closes the roster, confirms on behalf of an unreached worker, hands unclear rows to the custodian. |
| **Worker** | `/worker/` | The worker | Their record and what is waiting for their say; what counts as done and what it pays; money with every hold explained and owned; credentials in an encrypted device wallet, exportable and restorable; who checked them; consent per share; recovery contacts. |
| **Verify** | `/verify/` | Anyone, with no account; onboarded institutions | Checks a credential from a scan or a pasted document, online against the status list and trust chain or offline from the signature; institutional batch checks and share requests. |

## What each holds on the device

| Door | On the device | Why |
|---|---|---|
| Enrolment | An encrypted IndexedDB queue of registrations made offline, and their completion history | Registration in the field happens without signal; the queue is the agent's, the record is the service's |
| Worker | An encrypted wallet holding the person's credentials, restorable from an export | The credential is the worker's to carry; the service holds the record, not the only copy |
| Verify | Nothing beyond the issuer's public key and the last status list it fetched | A verifier needs no account and no session |

## The demonstration face

`apps/web` is the earlier face over the same API: six personas, a scripted story week, and a landing page that names what is deliberately undrawn. It is kept as a reference; [../DEMO.md](../DEMO.md) records what it shows and refuses to fake.

