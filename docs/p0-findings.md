# P0 findings memo (#4)

Where the substrates differ from what the design assumed. **In progress** — #2 has landed, #1 and #3 are partial. Phase 1 schema decisions stay open until this memo closes.

Each entry names the blueprint section it touches and the correction it forces. Entries that only cost us operational work are marked as such; entries that change the design are not.

Last updated 2026-08-22.

---

## Registry — DeDi (#2) · Blueprint §3 · **spike passing**

Full write-up: [spikes/dedi.md](spikes/dedi.md).

The substrate does what §3 assumes. Publish, inclusion proof, independent validation, versioning and compare-and-swap writes all work, and a proof issued against v1 still validates after v2 is published — which is the property credentials depend on.

| # | Finding | Effect |
|---|---|---|
| R1 | ~~No published DeDi image~~ — **withdrawn, this was wrong.** It is published as `flywheelai/dedi-node` (multi-arch, actively pushed). I checked `ghcr.io/theflywheel/dedi-node`, found nothing, and generalised from one registry to "no image exists" | None. Kept rather than deleted: a memo that quietly loses its wrong entries cannot be trusted for its right ones. |
| R2 | DeDi expects its own Postgres | Operational, and correct: the log's checkpoints must not be reachable by a CREST migration. |
| R3 | **An unrecognised query parameter is ignored, not rejected.** `?versionId=` returns the *latest* version with a valid proof attached; the parameter is `version_id` | **Design.** See below. |
| R4 | `If-Match` wants `<digest>-<state>`, but lookup returns those as two fields with no combined tag | Minor. Clients re-derive a format the server should own. |
| R5 | DeDi-node needs Go 1.25; CREST is on 1.24 | None. Separate modules, HTTP between them. Forecloses vendoring, which is right anyway. |
| R6 | **A node mints its identity key on first boot, from whatever `DEDI_ORIGIN` it has then.** Ours booted before the origin was set, so its checkpoints were signed under `dev.dedi.local/log` while claiming the deployment's origin | Operational, and sharp: the signer name is what ties a log to a deployment. Fixed by wiping the node's database and letting it re-mint. **Set `DEDI_ORIGIN` before first boot, never after.** |
| R7 | The published manifest (`/.well-known/dedi.index.json`) is regenerated on a schedule, so just after a key change it advertises the **previous** key | Operational. Presents as "signature does not verify" with no clue why. `tools/spikes/dedi-verifier-key.py` cross-checks the advertised key against the key that actually signed the checkpoint, and says which case it is. |

**R3 is the finding with teeth.** A verifier that misspells the version pin silently resolves a *different* definition and receives a perfectly valid proof for it. Nothing in the response says "you did not get what you asked for". Applied to CREST, that is a verifier checking a credential against the wrong version of the work definition and having no way to notice.

This does not contradict §3, it adds a requirement to it: **CREST's registry interface must reject unknown query parameters rather than answer a different question.** Recorded against #20, where the DeDi abstraction is built.

**Not covered:** consistency proofs and witnessing. Inclusion proves a record is in *a* log; consistency proves the log was never rewritten, and that is the property an independent verifier actually leans on. Deferred to #20 and named here so it is not mistaken for done.

---

## Credential substrate — Inji (#1) · Blueprint §5 · **partial**

The image audit is complete and it changed the picture. The issuance demo is not done.

| # | Finding | Effect |
|---|---|---|
| C1 | **`mosipid/inji-verify` is not a component.** Our pinned tag `0.11.0` does not exist — that repository stops at 0.10.0, amd64-only, last pushed October 2024. The live components are `inji-verify-service` and `inji-verify-ui`, two images on their own version line (0.16.0) | Corrected in compose. Had we not checked, the first attempt to bring up the stack would have failed on a component nobody could find. |
| C2 | `inji-verify-ui` publishes **amd64 only** | Runs under emulation on Apple Silicon. Fine for a UI, not fine for anything we measure. Pinned explicitly so it is a stated choice. |
| C3 | Every other tag we had guessed was stale but real: certify 0.11.0 → **0.13.1**, esignet 1.5.0 → **1.8.0**, mock-identity 0.10.0 → **0.13.0**. All publish arm64 | Corrected in compose; all four pulled and verified. |
| C4 | Certify's `local` profile is genuinely self-contained: a PKCS12 softHSM keystore, `spring.cache.type=simple`, no Spring Cloud Config server. **Redis is present on the classpath but commented out of the configuration** — it is not required | Good news, recorded because "Spring app, therefore Redis" was the assumption worth checking. It does not disturb "no Redis" in COMPONENTS.md, which is about CREST's own services. |
| C5 | Certify needs database `inji_certify`, schema `certify`, and **`ddl-auto=none`** — the DDL is applied from the upstream repo's `db_scripts`, not created on boot | Operational, and it must be wired into compose init before #1 can proceed. |
| C6 | Certify's local profile points its authorization server at eSignet on `localhost:8088` | Certify and eSignet come up together or not at all. Affects how #1 and #3 are sequenced: they are one bring-up, not two. |

**Still open on #1:** issuing a hand-authored `WorkEventCredential` over OpenID4VCI, holding it in a wallet, rendering a printed card via PixelPass, and verifying that card **fully offline**. The offline verification is the part that matters — it is W6 — and it needs a real device with its radios off. A container asserting it has no network is weaker evidence, and the honest place to prove it is the field simulation the test manifest already schedules.

---

## Identity — eSignet (#3) · Blueprint §4 · **partial — deployed and answering**

~~None of that can be answered against a local mock.~~ **That claim was wrong, and worth correcting rather than quietly dropping.** It conflated two different questions. Whether the subject identifier is pairwise is a property of eSignet's *code*, and self-hosted eSignet is the same code a sandbox runs — so it is answerable by anyone willing to deploy it. What genuinely needs an outside party is only the second half of #3's "done when": production access, who to talk to, and lead time. That half is still open, and like the rail sandbox (#19) it is a conversation to start now.

eSignet 1.8.0 and mock-identity 0.13.0 now run on Railway against their own logical databases. Discovery is public:

    https://crest-esignet-production.up.railway.app/v1/esignet/oidc/.well-known/openid-configuration

**E1 — `subject_types_supported` is `["pairwise"]`, and only that.** eSignet advertises no `public` option, which is stronger than §4.1 assumed: pairwise is not a mode CREST selects and could misconfigure, it is the only mode on offer. **This is not yet the confirmation #3 asks for.** What a relying party is *told* is supported and what it *receives* across two sessions are different claims, and only the second one is evidence. The OIDC round-trip is the remaining work.

**E2 — `claims_supported` includes `individual_id`.** The claim set is `name, address, gender, birthdate, picture, email, phone_number, individual_id, phone_number_verified, registration_type, updated_at`. `individual_id` is a national identifier: **requesting it would put CREST one careless persist away from breaking W9.** The scope-to-claim mapping is configuration, so this belongs in the relying-party registration as an explicit exclusion, not in a code review later. §4.1 should name the claims CREST requests and say plainly that `individual_id` is not one of them.

**E3 — the issuer is whatever `mosip.esignet.host` says, and it defaults to `localhost:8088`.** The local profile hard-codes it, so a deployment that does not override it publishes a discovery document advertising `http://localhost:8088` as its issuer — served over a public HTTPS URL, and self-consistent enough to look fine. Same shape as R6 for DeDi: **an identity that defaults to a development value and is not checked against where the service actually is.** Set `MOSIP_ESIGNET_HOST` and `MOSIP_ESIGNET_DOMAIN_URL` before first boot; verifying deployment means fetching the discovery document and comparing `issuer` to the host you fetched it from.

**E4 — keys are minted on first boot and the alias survives a keystore change.** eSignet and mock-identity both write `key_alias` rows on startup and load the material from the configured keystore. Change the keystore backend and the aliases persist while the material does not, and every subsequent boot dies on `Key in DBStore does not exist for this alias. So fetching the certificate from HSM.` — which names HSM even when no HSM is configured, so it reads like a missing dependency rather than the stale row it is. Recovery is truncating `key_alias` and `key_store`. **A third instance of the same class as R6 and E3**: substrate components mint an identity at first boot and give you no signal that it no longer matches the configuration around it. Worth stating as a rule — *for anything that mints its own identity, set the identity before first boot and verify it from outside afterwards.*

**E5 — the deployed stack uses a PKCS12 keystore, and says so.** The log carries `IT IS SUGGESTED NOT TO USE PKCS12 KEYSTORE TYPE IN PRODUCTION ENVIRONMENT`. That is fine for a spike and **not fine for a pilot**; it is the same question as G1 #7 (key custody), and the deployed environment is now a concrete argument for settling it. Recorded here so that no one later mistakes "it worked in the spike" for "the key handling was reviewed".

### C7 — the published eSignet quickstart cannot start on any host that is not root

Both images share an entrypoint (`configure_start.sh`) that installs a PKCS#11 HSM client. It skips that step only when the profile is **exactly** the string `local`:

    if [ "$active_profile_env" != "local" ]; then   # ... install HSM client ...

Every published quickstart, including MOSIP's own `docker-compose.yml`, sets `default,local`. That is not equal to `local`, so **the skip never fires and the installer always runs.** It ends in `sudo ./install.sh`, which needs a terminal; MOSIP's compose gets away with it only by running `user: root`. On any platform that will not let you override the container user — Railway among them — the container fails, and because `mv` collides with the directory the previous attempt left behind, it fails differently on each restart and never with a clear cause.

Dodging it by setting the profile to `local` alone does not work either: `application-local.properties` is a 55-line overlay on a 460-line `application-default.properties`, so `local` by itself is missing most of eSignet's configuration. It boots far enough to look promising and dies on the first `@Value` with no default (`mosip.esignet.header-filter.paths-to-validate`). **The profile CREST needs is the profile that triggers the bug**, so the entrypoint is what had to go: `infra/compose/Dockerfile.esignet` rebuilds both images with `esignet-start.sh`, which keeps the plugin-loading step and drops the HSM installer.

This is upstream's bug, not ours, and it is worth reporting. It also means **CREST is running eSignet in a configuration its maintainers do not test**, which is a thing to say out loud before a pilot rather than after.

---

## Deploying it found two more things

Running the same spike against a real deployment rather than a container on a laptop found R6 and R7 above, and one bug in **our own** code:

**`dediproof` split the verifier key on every `+`.** The key format is `name+hash+base64`, and standard base64 contains `+`. The local node's key happened not to, so nine tampering tests and every local run passed; the deployed node's key did, and it failed immediately. Fixed with `SplitN`, and the deployed key is now a regression test.

The lesson is not about base64. It is that **a test suite built entirely from one sample proves less than it appears to**, and the cheapest way to find out is to run against something you did not generate.

## What this memo already changes

- `infra/compose/docker-compose.yml` and `.env.example` — corrected images and topology (C1, C2, C3), DeDi's own database and key material (R1, R2).
- `Makefile` — `dedi-image`, `dedi-keys`, `spike-dedi`.
- A requirement on #20: reject unknown query parameters (R3).
- `docs/DEPLOYMENT.md` — the deployed environment, and what is *not* set up in it.

## What it does not change

No primitive, credential shape or evidence-contract decision moves on the strength of what is here. §3 stands as written; §5 and §4 have not yet been tested where they make their riskiest claims — offline verification and pairwise identity respectively. **This memo cannot close, and Phase 1 schemas cannot freeze, on an image audit.**
