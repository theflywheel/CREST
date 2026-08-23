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
| eSignet discovery | https://crest-esignet-production.up.railway.app/v1/esignet/oidc/.well-known/openid-configuration |
| Certify's credential offer | https://crest-certify-production.up.railway.app/v1/certify/.well-known/openid-credential-issuer |
| The issuer's own DID | https://crest-certify-production.up.railway.app/v1/certify/.well-known/did.json |
| What the wallet is offered | https://crest-mimoto-production.up.railway.app/v1/mimoto/issuers |
| The browser wallet | https://crest-inji-web-production.up.railway.app |
| The verifier | https://crest-verify-production.up.railway.app/v1/verify/actuator/health |

The checkpoint's first line is the log's origin, and it is this deployment's own domain. That is what anyone verifying CREST checks against, so it is tied to the deployment rather than to us.

`/healthz` reports the time from the **injected** clock, not `time.Now()`. A service that quietly reads wall-clock time shows up here rather than in a seven-day test that never fires.

## Where it lives, and why there

Everything sits in the existing **DeDi** Railway project. That is not tidiness, it is a constraint: Railway's private networking is per project and environment, so a separate CREST project could not reach `postgres.railway.internal` and would have to send database traffic over the public TCP proxy instead. Sharing the project is the cheaper and safer of the two.

The cost of that choice is worth stating: CREST services sit alongside a Beckn testnet in one dashboard, which makes it easier to delete the wrong thing. Moving them to their own project later is possible; it means giving up private networking to the shared Postgres, or running a second one.

| Service | What it is |
|---|---|
| `crest-dedi` | CREST's own DeDi node — its own log, its own origin, its own database |
| `crest-registry` | The registry service, built from `infra/compose/Dockerfile.service` |
| `crest-esignet` | eSignet 1.8.0, rebuilt with our own entrypoint (P0 finding C7). Databases `mosip_esignet`, `mosip_mockidentitysystem` |
| `crest-mock-identity` | MOSIP's mock identity system, which is what the spike authenticates against |
| `crest-esignet-ui` | eSignet's login UI **and eSignet's public hostname** — the API service alone does not serve the URLs eSignet's own discovery document advertises (P0 finding C12) |
| `crest-certify` | Inji Certify **0.14.0**, issuing `WorkEventCredential` over OpenID4VCI. Database `inji_certify`. 0.13.1 cannot accept eSignet's tokens at all (C10) |
| `crest-mimoto` | The wallet's backend-for-frontend. Database `inji_mimoto`; its keystore is on a volume, and must stay there (C14) |
| `crest-inji-web` | The browser wallet, and the origin that serves the issuer and verifier config documents Mimoto itself reads |
| `crest-verify` / `crest-verify-ui` | Inji Verify 0.16.0, service and UI |

**Three of these services hold a keystore that must outlive the container** — eSignet, Certify and Mimoto. Each has a Railway volume for exactly that reason: the key aliases live in the database, and a container that comes back with a fresh keystore fails to start with `No such alias`, in Mimoto's case *after* logging a successful start. Deleting one of those volumes is not a restart, it is a key loss.

**`crest-dedi` is deliberately a separate node from `dedid`.** CREST's work-definition log should not share a transparency log with a Beckn testnet demo: the origin, the key and the checkpoint history are what a verifier checks, and they should mean "CREST" and nothing else. It is on the same network and can be witnessed by the existing nodes, which is the part worth sharing.

### Two things about Railway's private network that cost a day each

**A service that binds `0.0.0.0` is unreachable.** The private network is IPv6-only,
so Spring services are started with `SERVER_ADDRESS=::`. The symptom is a timeout,
not a refusal, and nothing in either service's log mentions an address.

**nginx must be told to re-resolve.** It resolves a literal upstream once, at
configuration load, and holds that address forever; a redeployed service comes back
on a different private address, and every proxied request then hangs while the
upstream sits healthy on its own hostname. Both nginx-fronted services hold the
upstream in a variable with a `resolver` written at start-up from the container's own
`/etc/resolv.conf`. Note the second trap in the same fix: **once `proxy_pass` contains
a variable, nginx stops substituting the matched location prefix**, so it must be
passed `$request_uri` explicitly or every path under a location collapses onto one.

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

- **`crest-mock-identity` is on a public URL, and its create-identity endpoint is unauthenticated.** It holds synthetic fixtures only and is a mock by construction, but anyone can add one and then authenticate as it. It is exposed so `make spike-esignet` is runnable from a laptop against a fresh deployment; **it must not survive into anything a real worker touches**, and eSignet's own plugin only ever needs it over the private network.
- **eSignet's keys are in a PKCS12 keystore** the service itself warns against in production (P0 finding E5), and a registered relying-party client's public key cannot be rotated (C9). Both are inputs to the key-custody decision, G1 #7.
- **No staging environment.** `main` deploys straight to the only environment there is. That is honest for a pre-pilot project and unacceptable for a piloting one.
- **No rollback beyond Railway's own redeploy.** There is no tested "get back to the previous version" path.
- **No backups configured by us** on either new database, and no restore has been rehearsed. A backup nobody has restored from is a belief, not a backup.
- **No alerting.** The project has Grafana and Prometheus for the Beckn services; CREST is not wired into them.
- **The deploy is not reproducible from a commit alone** — Railway builds from an upload rather than from a registry image, so there is no digest tying a running container to a commit. That is the next thing to fix if provenance matters, and it will (see the `in-toto` side quest, #44).
