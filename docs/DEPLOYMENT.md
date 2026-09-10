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

`/healthz` reports this process's real time, in UTC. There is no injected clock any more (ruled 2026-09-09), and what the field is for now is comparison: two CREST processes that disagree about when it is move every worker's confirmation deadline, so `crest-core`'s health time and `crest-payments`' should read the same instant.

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
| `crest-certify` | Inji Certify **0.14.0**, issuing `WorkEventCredential` over OpenID4VCI. Database `inji_certify`. 0.13.1 cannot accept eSignet's tokens at all (C10). Since #155 phase C it issues from CREST live: the image carries `infra/certify/plugin`, selected by `CERTIFY_DATA_PROVIDER=CrestDataProviderPlugin`, which reads `/internal/certify/work-events` on crest-core over private networking (`CERTIFY_CREST_DATA_URL=http://crest-core.railway.internal:8080`, `CERTIFY_CREST_ISSUER=<eSignet UI origin>`). The CSV fixture (`CERTIFY_WORK_EVENTS_B64`, `make certify-bind`, finding C16) is retired from the deployed path — the variable stays honoured only for a stack that deliberately runs `CERTIFY_DATA_PROVIDER=MockCSVDataProviderPlugin` |
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

**The `attestation` schema is `crest-payments`' since #127.** Both services address the same database (`crest`), and the confirmation window's member moved from `crest-core` to `crest-payments` without moving a table: the schema is still named `attestation`, its tables keep their names, and its migration chain moved unrenumbered. So there is no data migration to run and nothing to back up before deploying it. What changes is which process migrates and writes it, and that flips the moment both services are on the new image.

Deploy order matters for one window only. `crest-core` on the new image no longer serves `/internal/windows`, and `crest-payments` on the new image does. Deploy **payments first**: a `claim.created` outbox delivery that arrives while core is new and payments is old retries — the outbox is at-least-once and the message is not lost — but a window that opens late is a worker asked late, so the shorter that gap the better. The two environment settings that must be right before either deploy:

- `crest-core`'s `CONFIRMATION_URL` must be `http://crest-payments.railway.internal:8080`. It was already required to be (see the #151 incident below); after #127 a self-pointed value silently opens no windows at all rather than eventually working.
- `crest-payments` needs `CONFIRMATION_WINDOW` (and `SWEEP_EVERY`, if the deployment wants the auto-confirm sweep, which every real one does). `crest-core` no longer reads either and can drop them.

**The window's opening instant crosses the same boundary, and its deploy order is the opposite way round (#221, ruled 2026-09-08).** `crest-core` on the new image adds `firstVisibleAt` to the `claim.created` handoff — the instant the worker could first have seen the record — and `crest-payments` opens the window at that instant rather than at whatever its own clock reads when the delivery lands. Deploy **payments first**, again, and for a harder reason this time than "sooner is better": every service refuses a body carrying a field it does not know, so a handoff from a new core to an old payments is a `400` the outbox retries until payments is upgraded. Windows would open, late, once it is — but the gap is a worker not asked, so keep it short.

A payload arriving *without* the field is handled the other way: payments falls back to its own arrival clock — the pre-#221 behaviour — and logs at warn that it did. **That fallback is for exactly one deploy.** It exists so the old core still opens windows while the two images are out of step, not as a supported configuration; `crest_window_opening_instant_fallbacks` on payments' `/internal/metrics` should be at zero once both services are on the new image, and a non-zero value there afterwards means something is sending a handoff that no CREST service writes.

- `crest-payments` takes `CLOCK_SKEW_ALERT` (default `5m`): how far apart core's supplied instant and payments' arrival clock may be before the disagreement is logged at warn and counted in `crest_window_clock_skew_events`. **That counter is the core↔payments clock-skew detector, and it should read zero.** A deployment whose two processes drift moves every worker's confirmation deadline, and before #221 there was no symptom anywhere. Note that a genuinely delayed delivery — an outbox retry through an outage — also raises it, because one message cannot tell the two apart; the log line names both instants so a person can.

**A credential that verified yesterday must verify today (#232, 2026-09-10).** `crest-core` audits the unrevoked credentials on record at boot against the keys it holds and refuses to start — "credentials on record carry verification methods this deployment holds no key for" — if any names a method it cannot answer. The pre-#207 method id `<ISSUER_ID>#key-1` is always answered under the current key; a deployment whose `#key-1` was a different key, or that rotated `ISSUER_SEED`, lists the old method and its public key in `ISSUER_HISTORICAL_KEYS_JSON` (`{"did:…#key-1":"z6Mk…"}`). The fleet carries that entry for the Sep 5 credentials.

**Checking at volume is capped, and the cap is not optional (G1 #9, 2026-09-10).** `crest-core` reads `CREST_VERIFY_RATE_CAP` (default `100`) and `CREST_VERIFY_RATE_WINDOW` (default `1h`): no requester — a verifier pass or a party — gets more online checks than that in any window, counted from the presentation trail, so the cap survives a restart and a second replica. A non-positive cap or a non-positive window falls back to the default with an error in the log, never to "unlimited". `CREST_VERIFY_BATCH_CAP` (default `100`) bounds one batch's size the same way. Bulk checking needs an active `verify-credentials-bulk` authorization on the requesting party; grant it through the console like any other function, and add it to the programme's Terms permissions first. Every online check names who is asking: a stranger gets a pass at `POST /v1/verifier-passes` with a name and a contact (J9 L1 — no account, no vetting), and the worker sees that name in their trail.

`CLOCK_DRIVEABLE` and `CLOCK_START` no longer exist anywhere in CREST (ruled 2026-09-09) — not as a variable a deployment could set, not as a route a process could serve. Every service reads real time. What a deployment configures instead is durations, and `pkg/service`'s deployment refusal rejects any of them that is not a positive duration: `CONFIRMATION_WINDOW` (the programme's window, `168h` for the CHW programme), `SWEEP_EVERY`, `SOURCE_MONITOR_EVERY`, `CLOCK_SKEW_ALERT`, `HELD_RETRY_EVERY`, `OUTBOX_RETRY_EVERY`, `CREST_RECOVERY_OVERRIDE_REVIEW`, `CREST_INVITE_TTL`, `CREST_PRESENTATION_REQUEST_TTL`, `CREST_BINDING_CACHE_TTL`. A window of zero is not a short window; it is a worker with no chance to object at all, which is why it is refused at start-up rather than discovered afterwards.

Because there is no clock to align, the two processes are held together only by the host's timekeeping. `CLOCK_SKEW_ALERT` (default 5m) and the `crest_window_clock_skew_events` counter on `/internal/metrics` are how a drifting fleet says so.

## How a change reaches production

```
PR → CI green → merge to main → CI on main → Deploy workflow → Railway → readiness poll
```

`.github/workflows/deploy.yml` triggers on CI *completing successfully* on `main`, not on the push. A pipeline that ships whatever landed regardless of tests is a way of finding out about failures from users.

The deploy ends by polling `/readyz` until the new version answers, and fails if it does not within ten minutes. A deploy step that returns without the service answering has told you nothing.

**The demo fleet (2026-08-25; six services since #129, 2026-08-28; two since #150, 2026-08-29).** *Railway matches this since 2026-08-29:* `crest-registry`, `crest-definitions`, `crest-evidence`, `crest-verification`, `crest-notify`, `crest-mock-sms`, `crest-docs` and the retired `crest-confirmation` service definitions were deleted (control plane; deploys were dark under the billing stop anyway), and `crest-core` was created carrying the live issuer identity (`ISSUER_ID`, `ISSUER_SEED`) copied from crest-verification before its deletion — so credentials already issued keep verifying when the fleet redeploys — with the member `*_URL`s self-pointed, `CLOCK_DRIVEABLE` absent (it had survived on two services against the #129 ruling; the variable no longer exists at all since 2026-09-09) and no `NOTIFY_URL`. crest-payments and crest-seed were repointed at crest-core. First deploy of crest-core happens when billing clears. Two services — `crest-core`, the one infrastructure deployable whose members are parties, definitions, evidence and verification, and `crest-payments`, the application — plus two mocks (`mock-oidc`, `mock-rail`; `mock-sms` retired with notify: notifications are dropped entirely, a recorded gap, Blueprint §16), a seeding *job* (`crest-seed`) and one public door now run on Railway. The seeder has had no resident deployment since 2026-08-27: the seeded world lives in Postgres, so the service exists only while a seed is running. To run one: `railway up --service crest-seed --ci --detach`; when it finishes, `railway down --service crest-seed -y` removes the deployment. Only `crest-web` has a public domain — https://crest-web-production.up.railway.app — an nginx that serves `apps/web` and proxies `/api/<service>/` over private networking — the four member names and `crest-confirmation` alias onto their real homes — refusing `/internal/*` at the door (the §16 service-identity fence). The demo fleet runs `CREST_ENV=railway` on real time (since 2026-08-28, #129: no deployed environment ever set `CLOCK_DRIVEABLE` — the demo included; since 2026-09-09 no environment can, because there is no such variable and no clock to drive) and fresh salts; `/api/crest-confirmation/*` remains a proxy alias for links already in the wild, answered by the payments application; it is a demo of the journeys, not the hardened deployment, and the two must not be conflated: the hardened path is still the matrix below.

**No mock issuer and no seeder on the fleet (ruled 2026-09-09, #155 phase 4).** The paragraph above describes the fleet as it stood before that ruling; this one supersedes it on both points. **eSignet is the only OIDC issuer the deployed fleet trusts.** `crest-core` and `crest-payments` carry it as the *primary* provider — `CREST_OIDC_ISSUER=https://crest-esignet-ui-production.up.railway.app`, `CREST_OIDC_JWKS_URL=https://crest-esignet-production.up.railway.app/v1/esignet/oauth/.well-known/jwks.json`, `CREST_OIDC_AUDIENCE=crest-rp-core-bde044fd` — and `CREST_OIDC_EXTRA_PROVIDERS` is **removed**, so no second issuer is accepted. The Railway services `crest-mock-oidc` and `crest-seed` were **deleted**. One mock is left on the fleet: `crest-mock-rail`, which the payments application's `http` provider still needs a rail to talk to until #26 lands a real connector.

The consequence is the point, not a side effect: **the deployed demo world is whatever real people create through the doors.** Nobody seeds it. A stranger with a browser registers an identity through eSignet, an organisation applies at the open door, a worker enrols — and that is the demo. `tools/seed` (`SEED_STORY=true go run ./tools/seed`) is a **local/e2e fixture only**; so is the compose `mock-oidc` service, and so are the Playwright suites that depend on either (`tests/e2e-apps/apps.spec.js`, `fidelity.spec.js`, `journeys.mjs` — each now refuses a non-local `BASE_URL` instead of failing obscurely against the fleet). Honesty note, unchanged: eSignet is real, but its identity backend here is `esignet-mock-identity` — the national registry behind the login is still the stand-in until a pilot geography grants relying-party access (#53).

**The hosted design docs (2026-08-25, folded into the apps door 2026-08-29, #148).** The markdown design docs rendered by [Quartz](https://github.com/jackyzha0/quartz) (pinned v4.5.2), with the self-contained HTML design docs (the blueprint among them) copied in beside the rendered pages so relative links resolve, served at **https://crest-apps-production.up.railway.app/docs/**. `docs/README.md` is the site's index. The build is a stage of `infra/railway/Dockerfile.apps` — Quartz is cloned at image build and nothing of it is vendored into this repo. To publish a docs change: `railway up --service crest-apps --detach` from the repo root. The site is public and unauthenticated; nothing lands in `docs/*.md` that cannot be read by a stranger. The separate `crest-docs` deployable was deleted on 2026-08-29.

The deploy matrix covers the whole demo fleet (2026-08-28, #138; `mock-oidc` dropped from it 2026-09-09, #155 phase 4): the services, the one remaining mock (`mock-rail`), and the doors, sequentially in that order — they share one database, and a failure part-way through a parallel fan-out leaves a mix of versions nobody can name. `make deploy-demo` is the audited manual fallback: the same loop in the same order, ending in `make verify-deployed`, which sweeps every service through the crest-web proxy, asserts the allowlist 404s unknown names and `/internal/*`, checks all seven doors, and still verifies the DeDi log independently and the eSignet issuer.

## Secrets

| Secret | Where it lives |
|---|---|
| `RAILWAY_TOKEN` | GitHub Actions secret, scoped to this project + environment |
| Postgres password for `crest` | Railway service variables only |
| DeDi publisher private key | Railway service variables only; the local copy is gitignored |
| DeDi node signing key | Minted by the node on first boot, kept in its own database, never exported |
| Inji Verify signing keystore | `make verify-keystore` mints it into `infra/verify/secrets/` (gitignored); on Railway it is `INJI_VERIFY_KEYSTORE_P12_B64` + `INJI_VERIFY_KEYSTORE_PASSWORD` as service variables |
| Service identities (`CREST_SERVICE_ID` + `CREST_SERVICE_PRIVATE_KEY` + `CREST_SERVICE_PEERS_JSON`) | **The fleet's service-to-service authentication since 2026-09-10.** One Ed25519 seed per calling service (`core`, `payments`, `certify`), and one peers document on every receiver naming each identity's public key and the `/internal/` operations it may call. Minted with `openssl genpkey -algorithm ed25519`; the seed is the last 32 bytes of the DER, base64; the peers document is `{"<id>":{"publicKey":"<base64 32 bytes>","allow":["METHOD /path"]}}`. Railway service variables only |
| `CREST_SERVICE_TOKEN` | The shared-token alternative for the same boundary, at least 32 bytes and the same value on sender and receiver. The local compose stack and the e2e harness use it; the fleet moved off it on 2026-09-10 because the Certify plugin only speaks the signed mode |

**One of the two is mandatory outside `CREST_ENV=local` since #207.** A service with neither logs `deployment configuration refused` and exits, and Railway restarts it forever while reporting the deployment as SUCCESS — the door then times out on `/readyz` and the deploy workflow fails on "did not answer within 10 minutes" with nothing pointing at the cause. That is how the demo fleet was dark from #207's merge until 2026-09-08. When a deploy fails readiness, read the service's logs before anything else: `railway logs --service crest-core --json | head`.

**Why the fleet runs the signed mode, precisely.** The Certify plugin (`infra/certify/plugin`) signs its call to core's `/internal/certify/work-events` with `CREST_SERVICE_ID` + `CREST_SERVICE_PRIVATE_KEY` and sends no shared token; a core running the token mode answers it 401, Certify turns that into "CREST's work-event surface answered 401", and the wallet shows "unable to download the card". Found 2026-09-10 walking #66's held leg. The peers document on `crest-core` and `crest-payments` allows `core` and `payments` everything under `/internal/` and `certify` exactly `GET /internal/certify/work-events`. `crest-certify` carries its identity only — it receives nothing.

### The wallet leg on Railway (#66 held, #155 phase C — proven 2026-09-10)

A worker signs in to Inji Web through eSignet, Certify issues from core's live work events, the card comes back as a PDF, and Inji Verify reads its QR and answers valid. Four settings made that true, each found by the walk failing without it:

- `crest-inji-web` builds from `infra/compose/Dockerfile.inji-web`, whose entrypoint writes `MIMOTO_URL` into `env.config.js` at start. A deploy from before that entrypoint served the baked `http://localhost:8099` to every browser.
- Its nginx proxies `/v1/mimoto/` to `crest-mimoto.railway.internal:8099` with a resolver and a variable upstream (`infra/injiweb/nginx.conf`), passes the wallet's own `Host`, and forwards `X-Forwarded-Proto`/`Host`/`Port`. Mimoto decides "cross-origin" by comparing the browser's `Origin` with the host and scheme it received, and refuses `Invalid CORS request` (403) before any handler runs when they differ.
- `crest-mimoto` carries `SERVER_FORWARD_HEADERS_STRATEGY=framework`, so Spring reads those forwarded headers instead of the plain-HTTP private hop.
- `crest-verify-ui` carries `VERIFY_SERVICE_API_URL=/v1/verify` — the path its own nginx proxies to the verifier — not the verifier's public URL, which this UI version concatenates onto its origin and fails to fetch.

Inji Verify's upload rejects files under 10 KB, and Mimoto's card PDF is about 5 KB; a phone camera scan or the QR re-rendered as a PNG passes. The card is signed `Ed25519Signature2020` by Certify's key (#164); the verifier's own signing key is CREST's (#65).

**None of these are in the repository**, and `.railwayignore` keeps key material out of the build upload as well — an upload is a copy, and a copy of a signing key is a signing key.

### The verifier signs with a key this deployment generated (#65, finding V1 — closed)

`mosipid/inji-verify-service:0.16.0` ships a PKCS#12 inside the image —
`BOOT-INF/classes/sample-keystore/test.p12`, alias `test`, password `mosip`,
all three public. That is the key it signs OpenID4VP authorization requests
with, so until this change a signed verifier request identified nobody: anyone
who could `docker pull` could produce one.

**Why the earlier attempt to replace it broke start-up.** Not the alias, not the
path, not the password. `io.inji.verify.key.impl.P12KeyExtractor` enumerates the
keystore's aliases and takes the first key entry whose certificate's public key
algorithm is `Ed25519` or `EdDSA`; anything else throws, and the exception is
thrown from a `@PostConstruct` on `p12FileKeyManagementServiceImpl`, so the
whole Spring context fails and the container dies:

    Caused by: java.lang.Exception: No EdDSA key entry found in the P12 file.

A keystore made with `keytool -genkeypair` defaults is RSA, and that is the
error it produces. The alias may be anything.

**How CREST holds it now.**

    export INJI_VERIFY_KEYSTORE_PASSWORD="$(openssl rand -base64 24)"
    make verify-keystore          # infra/verify/secrets/verify-signing.p12, Ed25519

`infra/verify/verify-keystore.sh` refuses to run with the password unset, and
refuses the literal `mosip`. `infra/compose/Dockerfile.verify` replaces the
image's entrypoint with `infra/verify/verify-start.sh`, which refuses to start
at all if there is neither a mounted keystore nor `INJI_VERIFY_KEYSTORE_P12_B64`
— falling back to the published key is not a degraded mode, it is the finding.
Compose sets `INJI_KEYSTORE_FILE_PATH` and `INJI_KEYSTORE_FILE_PASS` (both
`@Value`-injected upstream, so relaxed environment binding reaches them), and
`INJI_KEYSTORE_FILE_PASS` carries the empty string, never `mosip`.

**What actually happens with no password set, precisely.** Compose itself does
*not* fail: `INJI_VERIFY_KEYSTORE_PASSWORD` is interpolated as `${…:-}`, so
`docker compose config` and `docker compose up` both succeed, and the
**container** exits 1 with

    verify: INJI_KEYSTORE_FILE_PASS is unset or is the published default.

A `${…:?}` would be louder, and was tried and rejected: compose interpolates
the whole file on every invocation, so an unset variable there breaks
`make harness-up`, `make apps-up` and CI's `docker compose config -q` for
services that have nothing to do with the verifier. The refusal belongs where
it can see only this container. `make substrate-up` depends on
`make verify-keystore`, so the ordinary path never reaches that error.

On Railway, `crest-verify` builds from the same `infra/compose/Dockerfile.verify`
and needs no volume — unlike eSignet, Certify and Mimoto, this keystore records
no alias in a database, so re-materialising the same secret is a complete
restore. Set two service variables:

    INJI_VERIFY_KEYSTORE_P12_B64=$(base64 < infra/verify/secrets/verify-signing.p12 | tr -d '\n')
    INJI_KEYSTORE_FILE_PASS=<the password used to generate it>

**Proof that the running service uses it.** The service publishes the public
half at `/v1/verify/did.json`. The multibase there decodes to the same 32 bytes
`make verify-keystore` printed:

    $ curl -s http://localhost:58091/v1/verify/did.json | jq -r .verificationMethod[0].publicKeyMultibase
    z6MkiHfAvat8Z76mZL8eMJBuGeF5QQihhXfo75cXjVCYLxrb
    # base58btc-decoded: ed01 || 38f916b9…c06d05c0, and the generator printed
    #   public key (hex): 38f916b9185a0c4e37363b9a336e72ba6f367ea2d20b3507b62b220ec06d05c0

**What is still not closed.** This makes the verifier's *request* signature
attributable. It does not make Inji Verify's **verification result** a signed
object — 0.16.0 returns `verificationStatus` as plain JSON over the wire, with
no signature of any kind. A payer relying on that result is relying on the
transport and on trusting the operator, not on a signature. Naming that is the
honest state; see the note the verifier demo carries.

The node's **verifier** key is public and meant to be: `./tools/spikes/dedi-verifier-key.py <url>` derives it and cross-checks that the key the node advertises is the key that actually signed the current checkpoint.

### OpenID4VP on Railway (#27 — verifier side proven 2026-09-10)

`crest-verify-ui` carries `VP_SUBMISSION_SUPPORTED=true`, so its "VP Verification" tab creates a request against `crest-verify` (`POST /v1/verify/vp-request`, client id `did:web:crest-verify-production.up.railway.app:v1:verify`), shows a QR of `openid4vp://authorize?client_id=…&request_uri=…`, and polls `/vp-request/{id}/status`. The request object behind the `request_uri` is a JWT signed EdDSA with the CREST-held verify key (`…:verify#key-0`, #65) naming `response_uri https://crest-verify-production.up.railway.app/v1/verify/vp-submission/direct-post`, `response_mode direct_post`, and the presentation definition from `infra/verify/config.json` (`ldp_vc`, `DataIntegrityProof`, type `WorkEventCredential`). A wallet posting an `ldp_vp` holding a Certify-issued Work Event credential gets `200 {"redirect_uri":…}`, the UI's poll flips to `VP_SUBMITTED`, `/vp-result/{txn}` reports the credential valid, and the screen reads "Congratulations, the given credential is valid!" with the credential's fields. `make openid4vp-present REQUEST=… CRED=…` is that wallet; the walk was done against a live UI request on 2026-09-10 with the credential Certify issued the day before.

**The wallet in that walk is a script, not Inji Web.** The deployed Inji Web 0.15.0 has the wallet-side flow, but it talks to Mimoto routes (`/wallets/{id}/presentations/…`, `/wallets/{id}/verifiers/…`) that exist from Mimoto 0.20.0, and `crest-mimoto` is pinned at 0.19.2 with the PDF patch. Upstream's Inji Web 0.15.0 compose ships `mosipid/mimoto:0.20.0`. That upgrade, and a `mimoto-trusted-verifiers.json` naming the verify service's DID client id and `…/vp-submission/direct-post` (the current file names the UI origin and a `vp-direct-post` path the request object never carries), is [#230](https://github.com/theflywheel/CREST/issues/230).

## What is not set up

Being explicit, so none of this is mistaken for done:

- **`crest-mock-identity` is on a public URL, and its create-identity endpoint is unauthenticated.** It holds synthetic fixtures only and is a mock by construction, but anyone can add one and then authenticate as it. It is exposed so `make spike-esignet` is runnable from a laptop against a fresh deployment; **it must not survive into anything a real worker touches**, and eSignet's own plugin only ever needs it over the private network.
- **eSignet's keys are in a PKCS12 keystore** the service itself warns against in production (P0 finding E5), and a registered relying-party client's public key cannot be rotated (C9). Both are named pilot blockers under #130; the custody mechanism they migrate into is below.

**The real login (#155 phase A, 2026-09-02).** The doors log in through eSignet: the parties member serves `GET /v1/auth/login` (redirect to eSignet's UI with PKCE) and `GET /v1/auth/callback` (code exchange with a `private_key_jwt` assertion; the browser gets the access token in a fragment, and the pairwise derivation never leaves the server — the dev issuer's `/dev/pairwise` has no production counterpart). To enable it on `crest-core`:

1. `go run ./tools/esignet` — mints the relying-party key. **The key is the client identity (finding C9)**: store the PEM as `ESIGNET_CLIENT_KEY` on crest-core (Vault later, per #130's pattern) and never regenerate it casually.
2. Set `ESIGNET_URL=https://crest-esignet-production.up.railway.app`, `ESIGNET_UI_URL=https://crest-esignet-ui-production.up.railway.app`, `CREST_AUTH_DOORS=<comma-separated door origins>` (exactly the origins, no trailing slash — the allowlist is what stops an open redirect).
3. **Make eSignet the primary provider (2026-09-09, #155 phase 4 — the roles have now swapped and mock-oidc is gone entirely):** `CREST_OIDC_ISSUER=https://crest-esignet-ui-production.up.railway.app`, `CREST_OIDC_JWKS_URL=https://crest-esignet-production.up.railway.app/v1/esignet/oauth/.well-known/jwks.json`, `CREST_OIDC_AUDIENCE=crest-rp-core-bde044fd`, and **`CREST_OIDC_EXTRA_PROVIDERS` removed** — on `crest-core` *and* `crest-payments`, which must agree about who a token's issuer may be. eSignet is the deployed fleet's only trusted issuer; the dev issuer lives on in compose and the e2e harness and nowhere else.

   `CREST_OIDC_EXTRA_PROVIDERS` remains the mechanism for a deployment that genuinely needs a second issuer: one entry per issuer, `issuer|jwks_url|audience` — the third field matters for eSignet, whose access tokens carry the relying-party **client id** as `aud` (so: `|crest-rp-core-…`); omit it and the provider inherits the primary's audience. A malformed entry refuses startup rather than silently dropping a provider. Adding an issuer widens who can be believed, so it is a decision with an owner, not a convenience.

The service registers its eSignet client at boot (idempotent; registration failure is logged and retried next boot, not fatal). eSignet's identity backend in this fleet is `esignet-mock-identity` — the login is real, the national registry behind it is the stand-in until a pilot geography (#53).

**Every MOSIP keymanager service needs its keystore on a volume — mock-identity included.** Finding #4 (aliases in the database, PKCS12 on the container filesystem) applies to `crest-mock-identity` exactly as it does to eSignet, and it took the service down in production on 2026-09-02 when Railway recycled the container: the aliases outlived the keystore and boot died with `No such alias`. The fix is the same shape as eSignet's: a volume at `/home/mosip/keys`, `MOSIP_KERNEL_KEYMANAGER_HSM_CONFIG_PATH=/home/mosip/keys/mock_local.p12`, and — for a recovery where the split has already happened — delete the stranded `mockidentitysystem.key_alias`/`key_store` rows before restarting (mock keys sign nothing durable; eSignet's or Certify's must never be reset this way).

**Live issuance through Certify (#155 phase C, 2026-09-02).** The wallet path issues from CREST's own record: `crest-certify`'s image (rebuilt from `infra/compose/Dockerfile.certify`, which now maven-builds `infra/certify/plugin`) runs `CrestDataProviderPlugin`, reading `GET /internal/certify/work-events?issuer=&subject=` on the verification member over private networking. Rolling it onto the fleet:

1. On `crest-certify`: set `CERTIFY_CREST_DATA_URL=http://crest-core.railway.internal:8080` and `CERTIFY_CREST_ISSUER=https://crest-esignet-ui-production.up.railway.app`; **delete `CERTIFY_WORK_EVENTS_B64`** (the C16 fixture binding — with the CSV plugin retired it is dead weight, and leaving it invites someone to think it still feeds issuance). `railway up --service crest-certify --ci --detach` for the fresh image.
2. One-time SQL on `inji_certify` (`certify.credential_config`): set `signature_crypto_suite='Ed25519Signature2020'` and re-point the template's second `@context` entry at `https://crest-apps-production.up.railway.app/contexts/work-event-v1.json`, and set `did_url` to the deployment's issuer DID **ending in `:.well-known`** (see the walkthrough note below) — the row values are in `infra/compose/initdb/03-certify.sql`, which is the source of truth a fresh deployment gets automatically. The suite change is finding #164: Inji Verify implements no DataIntegrityProof suite, so the wallet path signs the one it can check; CREST's own credentials keep `eddsa-jcs-2022`.
3. The context document must be live **before** the first issuance — it ships in the apps door image (`apps/contexts/work-event-v1.json`), so deploy `crest-apps` first.
4. Prove it: `make certify-issue` (deployed defaults) — the credential's facts must be a confirmed claim's, not the CSV's, and Inji Verify must answer VALID.

**What the deployed walkthrough surfaced (2026-09-02).** Three fleet corrections, all found by walking the demo end to end against production and each one config, not code:

- **`crest-payments` never had `CREST_OIDC_EXTRA_PROVIDERS`.** The registry accepted eSignet-issued worker tokens; the payments application (windows, confirm, instructions) answered 401 to the same token. The variable must carry the same `issuer|jwks_url|audience` entry as `crest-core`.
- **`crest-core`'s `CONFIRMATION_URL` still pointed at the retired `crest-confirmation` name** (#151 consolidation). Every `claim.created` outbox delivery retried into a DNS failure, so no confirmation window opened for any claim ingested since the consolidation — a claim that exists with no window is a worker never asked and never paid. It must be `http://crest-payments.railway.internal:8080`.
- **The issuer DID must end in `:.well-known`.** Inji Verify's `DidWebPublicKeyResolver` resolves a pathed `did:web` at the spec location `https://host/<path>/did.json` only; Certify serves the document at `<path>/.well-known/did.json` and answers the spec path with an HTTP-200 auth-error envelope, which the resolver parses and rejects ("Verification method not found"). The fix is to make the DID's spec-path resolution land where the document actually is: `CERTIFY_DID_URL=did:web:<host>:v1:certify:.well-known` **and** the same value in `certify.credential_config.did_url` (the column feeds `proof.verificationMethod`; the env var feeds `issuer` and the served document — they must move together, and a restart is needed for the row to be re-read). Compose and `initdb/03-certify.sql` now carry this shape, so a fresh stack gets it automatically.

The proof of the whole path is `INDIVIDUAL_ID=<id> PIN=<pin> make certify-issue` for an identity whose work was confirmed through the doors: all eleven assertions pass, including `Inji Verify accepts the credential (verificationStatus=SUCCESS)`. The recorded, reproducible walkthrough is `tests/e2e-apps/demo-e2e.mjs`.

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

### Standing up from nothing: the operator, and the doors after it

A clean registry has no party in it, and every door answers to one — the
operator approves the first organisation, an approved organisation invites
its people, a worker enrols themselves. So the operator is the one party that
cannot arrive through a door, and stand-up writes it (Blueprint §15 G-1,
"the first screen anyone ever sees" is deploy-time):

```sh
CREST_INSTANCE_ID=<this deployment's id> DATABASE_URL=<the core database> \
    go run ./tools/bootstrap-operator \
    -name "CREST production operator" -email ops@example.org \
    -door https://crest-console-production.up.railway.app
```

It prints the operator's party id, a one-time claim code, and the console
link that carries it. It also records the deploy-time approval (the same
`instance_setup` and APPROVED registration rows first-run setup writes, which
is why it needs `CREST_INSTANCE_ID`): without that record the operator is an
organisation of the right shape and no authority, and cannot grant anything
(found on the fleet 2026-09-10). A deployment stood up before that fix gets
the record written at core's next boot, with a warning in the log, and its
operator organisation published to the registry the same way. Set `CREST_OPERATOR_PARTY_ID` to the id and redeploy
`crest-core`; then open the link and sign in with eSignet. That first login
claims the operator's record — the same append-only identity binding as any
other, put in front of an invitation instead of the bare first-login
bootstrap (finding #123; `services/core/parties/partyinvites.go`).

From there nothing else is seeded. An organisation registers at the open
onboarding door and receives its own claim code (g2_13's "copy the key");
the operator approves it in the console; the organisation invites each person
from People & roles, which creates their record, grants their role, and
shows a claim link once; the person signs in with eSignet from that link.
Workers self-register on the worker door. The story seeder (`tools/seed`) is
a **local/e2e fixture** and is not part of stand-up; since 2026-09-09 (#155
phase 4) it has no deployed counterpart at all — the `crest-seed` Railway
service was deleted, and the demo world on the fleet is exactly what real
people created through these doors.

**Wiping the demo fleet (authorised 2026-09-05, this window).** The
database has no public endpoint and the CLI's ssh does not reach Railway's
Postgres, so both deploy-time acts run as a one-off job service. It used to
be `crest-seed`, which was deleted on 2026-09-09 (#155 phase 4); create a
job service of your own — `crest-bootstrap` below — built from
`infra/compose/Dockerfile.tool` with
`TOOL=bootstrap-operator`, `TOOLDIR=tools` and `DATABASE_URL` referencing
the Postgres service. `BOOTSTRAP_MODE=wipe` drops exactly the five CREST
schemas (`parties`, `definitions`, `evidence`, `verification`, `payments`;
each service re-creates its own from embedded migrations on boot, and the
identity provider's schema in the same database is never touched). Then
`railway redeploy` `crest-core` and `crest-payments`, run the job again
without `BOOTSTRAP_MODE` and with `BOOTSTRAP_NAME` / `BOOTSTRAP_EMAIL` /
`BOOTSTRAP_DOOR` set, read the operator's id and claim link from
`railway logs --service crest-bootstrap`, set `CREST_OPERATOR_PARTY_ID`,
redeploy `crest-core` once more, and `railway down --service crest-bootstrap
-y` — the job must not outlive the act that needed it.
Credentials already issued stop verifying against a registry that no longer
holds their parties — a wipe is a new deployment wearing the old hostnames,
and it must be treated as one.

One caveat worth stating: `publisherKeyId` is **self-asserted**. Anyone holding a
valid publisher key for the namespace could publish a different answer. What
stops that being silent is the log itself — the record is append-only, so a
change is visible to anyone who looked before. It is a fact you can watch, not
one you can take on faith.
