# Deployment

There is one deployed environment: **production on Railway**. It exists early on purpose — a deploy path proven while the services are trivial is a deploy path that will not need debugging on the day something real depends on it.

## Verify it yourself, in one command

```bash
make verify-deployed
```

That checks both services answer, then fetches a real work definition **with its inclusion proof and validates the proof with our own verifier** — not by asking the node whether it agrees with itself.

Or in a browser, in this order, because each link is checkable against the next:

| | |
|---|---|
| The registry node | https://crest-dedi-production.up.railway.app |
| Its signed checkpoint | https://crest-dedi-production.up.railway.app/dedi/log/checkpoint |
| A work definition, with proof | [WD-4471](https://crest-dedi-production.up.railway.app/dedi/lookup/crest/work-definitions/WD-4471?proof=inclusion) |
| Version 1 of it, still resolving | [WD-4471 v1](https://crest-dedi-production.up.railway.app/dedi/lookup/crest/work-definitions/WD-4471?version_id=2&proof=inclusion) |
| The registry service | https://crest-registry-production.up.railway.app/healthz |

The checkpoint's first line is the log's origin, and it is this deployment's own domain. That is what anyone verifying CREST checks against, so it is tied to the deployment rather than to us.

`/healthz` reports the time from the **injected** clock, not `time.Now()`. A service that quietly reads wall-clock time shows up here rather than in a seven-day test that never fires.

## Where it lives, and why there

Everything sits in the existing **DeDi** Railway project. That is not tidiness, it is a constraint: Railway's private networking is per project and environment, so a separate CREST project could not reach `postgres.railway.internal` and would have to send database traffic over the public TCP proxy instead. Sharing the project is the cheaper and safer of the two.

The cost of that choice is worth stating: CREST services sit alongside a Beckn testnet in one dashboard, which makes it easier to delete the wrong thing. Moving them to their own project later is possible; it means giving up private networking to the shared Postgres, or running a second one.

| Service | What it is |
|---|---|
| `crest-dedi` | CREST's own DeDi node — its own log, its own origin, its own database |
| `crest-registry` | The registry service, built from `infra/compose/Dockerfile.service` |

**`crest-dedi` is deliberately a separate node from `dedid`.** CREST's work-definition log should not share a transparency log with a Beckn testnet demo: the origin, the key and the checkpoint history are what a verifier checks, and they should mean "CREST" and nothing else. It is on the same network and can be witnessed by the existing nodes, which is the part worth sharing.

### Databases

Two new logical databases on the **existing** Postgres, owned by a non-superuser `crest` role:

- `crest` — the seven service schemas from `infra/compose/initdb/01-schemas.sql`, one per service, no cross-schema foreign keys.
- `crest_dedi` — the node's own log. **Separate on purpose:** the log's checkpoints must never roll back, and a shared database would put registry history within reach of a CREST migration.

Nothing runs as `postgres`. A payments-adjacent system that connects as a superuser has no story for "what could this service have done".

## How a change reaches production

```
PR → CI green → merge to main → CI on main → Deploy workflow → Railway → readiness poll
```

`.github/workflows/deploy.yml` triggers on CI *completing successfully* on `main`, not on the push. A pipeline that ships whatever landed regardless of tests is a way of finding out about failures from users.

The deploy ends by polling `/readyz` until the new version answers, and fails if it does not within ten minutes. A deploy step that returns without the service answering has told you nothing.

Only `crest-registry` is in the deploy matrix. The other six services are health-check stubs today; adding them costs money to prove something one of them already proves. They join the matrix as they gain behaviour.

## Secrets

| Secret | Where it lives |
|---|---|
| `RAILWAY_TOKEN` | GitHub Actions secret, scoped to this project + environment |
| Postgres password for `crest` | Railway service variables only |
| DeDi publisher private key | Railway service variables only; the local copy is gitignored |
| DeDi node signing key | Minted by the node on first boot, kept in its own database, never exported |

**None of these are in the repository**, and `.railwayignore` keeps key material out of the build upload as well — an upload is a copy, and a copy of a signing key is a signing key.

The node's **verifier** key is public and meant to be: `./tools/spikes/dedi-verifier-key.py <url>` derives it and cross-checks that the key the node advertises is the key that actually signed the current checkpoint.

## What is not set up

Being explicit, so none of this is mistaken for done:

- **No staging environment.** `main` deploys straight to the only environment there is. That is honest for a pre-pilot project and unacceptable for a piloting one.
- **No rollback beyond Railway's own redeploy.** There is no tested "get back to the previous version" path.
- **No backups configured by us** on either new database, and no restore has been rehearsed. A backup nobody has restored from is a belief, not a backup.
- **No alerting.** The project has Grafana and Prometheus for the Beckn services; CREST is not wired into them.
- **The deploy is not reproducible from a commit alone** — Railway builds from an upload rather than from a registry image, so there is no digest tying a running container to a commit. That is the next thing to fix if provenance matters, and it will (see the `in-toto` side quest, #44).
