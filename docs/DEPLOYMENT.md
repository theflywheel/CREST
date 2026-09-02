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
| The parties service (né registry, #50; the Railway service keeps the name `crest-registry` until it is renamed in the dashboard) | https://crest-registry-production.up.railway.app/healthz |
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
| `crest-registry` | The parties service (`SERVICE=parties`; renamed from registry, #50 — the schema renames itself on first boot via `FormerName`), built from `infra/compose/Dockerfile.service` |
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

**The demo fleet (2026-08-25; six services since #129, 2026-08-28; two since #150, 2026-08-29).** *Railway matches this since 2026-08-29:* `crest-registry`, `crest-definitions`, `crest-evidence`, `crest-verification`, `crest-notify`, `crest-mock-sms`, `crest-docs` and the retired `crest-confirmation` service definitions were deleted (control plane; deploys were dark under the billing stop anyway), and `crest-core` was created carrying the live issuer identity (`ISSUER_ID`, `ISSUER_SEED`) copied from crest-verification before its deletion — so credentials already issued keep verifying when the fleet redeploys — with the member `*_URL`s self-pointed, `CLOCK_DRIVEABLE` absent (it had survived on two services against the #129 ruling) and no `NOTIFY_URL`. crest-payments and crest-seed were repointed at crest-core. First deploy of crest-core happens when billing clears. Two services — `crest-core`, the one infrastructure deployable whose members are parties, definitions, evidence and verification, and `crest-payments`, the application — plus two mocks (`mock-oidc`, `mock-rail`; `mock-sms` retired with notify: notifications are dropped entirely, a recorded gap, Blueprint §16), a seeding *job* (`crest-seed`) and one public door now run on Railway. The seeder has had no resident deployment since 2026-08-27: the seeded world lives in Postgres, so the service exists only while a seed is running. To run one: `railway up --service crest-seed --ci --detach`; when it finishes, `railway down --service crest-seed -y` removes the deployment. Only `crest-web` has a public domain — https://crest-web-production.up.railway.app — an nginx that serves `apps/web` and proxies `/api/<service>/` over private networking — the four member names and `crest-confirmation` alias onto their real homes — refusing `/internal/*` at the door (the §16 service-identity fence). The demo fleet runs `CREST_ENV=railway` on the **live clock** (since 2026-08-28, #129: no deployed environment outside the e2e harness sets `CLOCK_DRIVEABLE` — the demo included; its story is seeded against real time), the mock OIDC issuer, and fresh salts; `/api/crest-confirmation/*` remains a proxy alias for links already in the wild, answered by the payments application; it is a demo of the journeys, not the hardened deployment, and the two must not be conflated: the hardened path is still the matrix below.

**The hosted design docs (2026-08-25, folded into the apps door 2026-08-29, #148).** The markdown design docs rendered by [Quartz](https://github.com/jackyzha0/quartz) (pinned v4.5.2), with the self-contained HTML design docs (the blueprint among them) copied in beside the rendered pages so relative links resolve, served at **https://crest-apps-production.up.railway.app/docs/**. `docs/README.md` is the site's index. The build is a stage of `infra/railway/Dockerfile.apps` — Quartz is cloned at image build and nothing of it is vendored into this repo. To publish a docs change: `railway up --service crest-apps --detach` from the repo root. The site is public and unauthenticated; nothing lands in `docs/*.md` that cannot be read by a stranger. The separate `crest-docs` deployable was deleted on 2026-08-29.

The deploy matrix covers the whole demo fleet (2026-08-28, #138): the seven services, the three mocks, and the seven doors, sequentially in that order — they share one database, and a failure part-way through a parallel fan-out leaves a mix of versions nobody can name. `make deploy-demo` is the audited manual fallback: the same loop in the same order, ending in `make verify-deployed`, which sweeps every service through the crest-web proxy, asserts the allowlist 404s unknown names and `/internal/*`, checks all seven doors, and still verifies the DeDi log independently and the eSignet issuer.

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
- **eSignet's keys are in a PKCS12 keystore** the service itself warns against in production (P0 finding E5), and a registered relying-party client's public key cannot be rotated (C9). Both are named pilot blockers under #130; the custody mechanism they migrate into is below.

**The real login (#155 phase A, 2026-09-02).** The doors log in through eSignet: the parties member serves `GET /v1/auth/login` (redirect to eSignet's UI with PKCE) and `GET /v1/auth/callback` (code exchange with a `private_key_jwt` assertion; the browser gets the access token in a fragment, and the pairwise derivation never leaves the server — the dev issuer's `/dev/pairwise` has no production counterpart). To enable it on `crest-core`:

1. `go run ./tools/esignet` — mints the relying-party key. **The key is the client identity (finding C9)**: store the PEM as `ESIGNET_CLIENT_KEY` on crest-core (Vault later, per #130's pattern) and never regenerate it casually.
2. Set `ESIGNET_URL=https://crest-esignet-production.up.railway.app`, `ESIGNET_UI_URL=https://crest-esignet-ui-production.up.railway.app`, `CREST_AUTH_DOORS=<comma-separated door origins>` (exactly the origins, no trailing slash — the allowlist is what stops an open redirect).
3. Accept eSignet's tokens beside the dev issuer's (the externally-shared PoC still logs in through mock-oidc): `CREST_OIDC_EXTRA_PROVIDERS=https://crest-esignet-ui-production.up.railway.app|https://crest-esignet-production.up.railway.app/v1/esignet/oauth/.well-known/jwks.json`. One entry per issuer, `issuer|jwks_url|audience` — the third field matters for eSignet, whose access tokens carry the relying-party **client id** as `aud` (so: `|crest-rp-core-…`); omit it and the provider inherits the primary's audience. A malformed entry refuses startup rather than silently dropping a provider. When the PoC retires, the roles swap and mock-oidc goes entirely.

The service registers its eSignet client at boot (idempotent; registration failure is logged and retried next boot, not fatal). eSignet's identity backend in this fleet is `esignet-mock-identity` — the login is real, the national registry behind it is the stand-in until a pilot geography (#53).

**Every MOSIP keymanager service needs its keystore on a volume — mock-identity included.** Finding #4 (aliases in the database, PKCS12 on the container filesystem) applies to `crest-mock-identity` exactly as it does to eSignet, and it took the service down in production on 2026-09-02 when Railway recycled the container: the aliases outlived the keystore and boot died with `No such alias`. The fix is the same shape as eSignet's: a volume at `/home/mosip/keys`, `MOSIP_KERNEL_KEYMANAGER_HSM_CONFIG_PATH=/home/mosip/keys/mock_local.p12`, and — for a recovery where the split has already happened — delete the stranded `mockidentitysystem.key_alias`/`key_store` rows before restarting (mock keys sign nothing durable; eSignet's or Certify's must never be reset this way).

**Key custody (#130, ruled 2026-08-29).** The CREST issuance seed lives in Vault — `crest-vault` on Railway (Raft storage on a volume, private networking only, never behind the proxy), dev-mode in compose so a clean local stack is reproducible. `verification` (the only signer since #137) reads it at startup via `VAULT_ADDR`/`VAULT_TOKEN`/`VAULT_SECRET_PATH` and refuses to start if Vault is sealed or missing the field — no silent fall-back to the environment. Operations that follow from this: `vault operator init` output (unseal key, root token) goes to the operator's escrow and nowhere else; **a Vault restart leaves it sealed** and verification unable to boot until `railway ssh crest-vault` + `vault operator unseal` — that is custody working, not an outage to automate away.
- **No staging environment.** `main` deploys straight to the only environment there is. That is honest for a pre-pilot project and unacceptable for a piloting one.
- **No rollback beyond Railway's own redeploy.** There is no tested "get back to the previous version" path.
- **No backups configured by us** on either new database, and no restore has been rehearsed. A backup nobody has restored from is a belief, not a backup.
- **No alerting.** The project has Grafana and Prometheus for the Beckn services; CREST is not wired into them.
- **The deploy is not reproducible from a commit alone** — Railway builds from an upload rather than from a registry image, so there is no digest tying a running container to a commit. That is the next thing to fix if provenance matters, and it will (see the `in-toto` side quest, #44).

## The registry substrate, and the key that writes to it

CREST's public facts — approved organisations, terms, authorizations held by
organisations, and every ACTIVE work definition — are published to the DeDi node
at `crest-dedi-production.up.railway.app` (#20, #21). Four variables select it,
and they are read by `registry` and `definitions` only:

| Variable | What it does |
|---|---|
| `DEDI_URL` | The node. **Empty selects the Postgres fallback**, which has no transparency log and therefore no inclusion proof |
| `DEDI_NAMESPACE` | `crest` — must match the node's `DEDI_WILDCARD_NAMESPACES` |
| `DEDI_KEY_ID` | `crest-services` |
| `DEDI_PUBLISHER_KEY` | The Ed25519 private key, base64. A secret |

**A URL with no key is refused at start-up**, deliberately. A deployment that
meant to publish to a transparency log and silently fell back to Postgres is the
worst of the three states, because every response still looks correct — which is
why `Receipt.Transparent` is carried through to the publication row and returned
to callers rather than being a deployment-wide fact nobody re-reads.

**The node's publisher keys are additive.** `DEDI_PUBLISHER_KEYS` on
`crest-dedi` is a comma-separated list of `kid:namespace:base64pubkey`. It
currently holds two: `crest`, minted during the P0 spike and whose private half
no longer exists anywhere, and `crest-services`, which the services use. Do not
replace the list when adding a key — appending is the whole point of the format,
and overwriting it silently revokes every other publisher.

To check a published fact independently:

```sh
make verify-registry REGISTRY=work-definitions RECORD=<the record id>
make verify-registry REGISTRY=organisations RECORD=<a party id>
```

The record id is the `record` field of `GET /v1/definitions/<id>/publication` or
`GET /v1/publications/organisation/<id>`. The target fetches the record with an
inclusion proof and hands it to `tools/spikes/dediproof` — a second
implementation written from the wire format, because asking DeDi to check its
own proof would only establish that DeDi agrees with itself.

## Telling a deployment who it is

`registry` publishes the deployment's own self-description to the `instances`
registry on the node (#70), so a verifier who resolves
`crest/organisations/<id>` can find out which deployment owns that namespace,
which publisher key its writes should carry, and who is answerable when a record
and a credential disagree. It is served unauthenticated at `GET /v1/instance` —
a public self-description nobody outside can read is one nobody outside can
check.

| Variable | Required | What it is |
|---|---|---|
| `CREST_INSTANCE_ID` | outside local | This deployment's identifier |
| `CREST_INSTANCE_NAME` | outside local | Human-readable name |
| `CREST_OPERATOR_PARTY_ID` | **always** | The organisation answerable for this deployment |

The first two have local-only defaults and none outside it, deliberately. A
deployment that invented its own identifier would publish under a name nobody
agreed to, and two that both defaulted would collide in one namespace — which on
an append-only log is not a mistake anyone can take back. The operator has no
default anywhere: it is the one field nobody can guess on someone's behalf.

The record is republished **when its content changes**, not when it is absent.
Bootstrap runs on every start, so publish-if-absent would freeze the first answer
forever, and a deployment that changed operator or rotated its publisher key
would go on advertising the old one — leaving the log, the thing a verifier
trusts, as the most confidently wrong copy in the system.

One caveat worth stating: `publisherKeyId` is **self-asserted**. Anyone holding a
valid publisher key for the namespace could publish a different answer. What
stops that being silent is the log itself — the record is append-only, so a
change is visible to anyone who looked before. It is a fact you can watch, not
one you can take on faith.
