# CREST development validation scope

The user clarified this scope on 2026-09-06. The three files in `docs/reference` remain the product requirements; this document records the implementation and validation decisions for the development run.

- Identity uses the actual eSignet service, discovery, JWKS, client registration, browser authorization and token exchange. Its upstream identity store is MOSIP's official development mock identity system. This is development identity validation, not national-ID integration acceptance. A fresh identity is created through that provider's registration API; existing story identities and direct CREST identity bindings are not reused.
- Notifications use the existing Sender contract and a small authenticated, durable HTTP inbox. SMS is deferred. Provider acceptance and viewing a message do not start a review window: the authenticated worker must separately acknowledge it, or an authorized assisted route must record reach.
- Payments remain a separate application microservice. Its provider catalogue selects an HTTP implementation or an explicitly development-only simulator. Submission must remain pending; settlement requires a separate authorized operation. Simulated settlement is never reported as real money movement.
- Evidence uses the CSV plugin only. Adapter discovery and version selection are explicit. New source classes are future adapter implementations, not blockers for this CSV development run. Plugins translate records; core retains consent, authorization, schema, attribution, deduplication and provenance enforcement.
- Existing eSignet/Certify/Mimoto/Inji containers are inspected and reused where appropriate. Running containers or successful health endpoints do not prove that Certify reads the clean core or that wallet issuance works; those paths must be verified explicitly.
- Production configuration, real notification delivery, real rail settlement and other source integrations remain separate future acceptance gates. They do not block this development run.

## Execution order

1. Wire the clean isolated core to the existing official eSignet service using its actual issuer/JWKS and a newly registered relying-party key. Keep private keys in Vault/private deployment files.
2. Start the HTTP inbox and the payments microservice with the development provider. Verify readiness, provider catalogues, unauthorized access denial and database separation.
3. Complete browser eSignet login for a fresh development operator. Configure that authenticated subject as the one-time setup administrator, then call the supported setup API. Establish further actors and permissions through ordinary authenticated registration/invitation workflows.
4. Create a project, consented worker, independently approved definition, registered CSV source and actual CSV submission through the supported interfaces. Trace the resulting unit and claim.
5. Verify notification receipt, worker acknowledgement and confirmation/dispute behavior. Use elapsed wall time; no clock jumps or privileged finish calls. The development deployment explicitly sets `CONFIRMATION_WINDOW=2m`; this exercises the timer without representing a production review period.
6. Verify credential custody/disclosure and an optional payment instruction. For the development payment provider, prove pending submission, a separate settlement action and retry/idempotency behavior.
7. Verify selected Certify/Mimoto/Inji issuance paths against this core. Record unsupported companion paths explicitly.
8. Exercise outages, retry recovery and offline behavior, then update the machine-readable run record. Do not seed CREST business tables or manufacture successful states with internal APIs.

## Extension boundaries

Evidence plugins implement `adapters.Adapter` and register a versioned factory in the catalogue. CSV is the initial implementation. Registration is static code registration; deployments cannot upload and execute arbitrary code through an API.

Payment plugins implement the provider contract inside the payments application. Provider selection is deployment configuration. Issuance and credential validity remain independent of the selected payment provider.

Notification providers implement `pkg/notify.Sender`; existing SMTP and HTTP transports remain available. The development inbox is replaceable through configuration and is not a production delivery guarantee.

## Local endpoints

- CREST interfaces: http://localhost:59510
- Clean core: http://localhost:59400
- Payments: http://localhost:59406
- Development notification inbox: http://localhost:59511/inbox (private token required)
- Existing eSignet UI/discovery: http://localhost:58088
- Existing eSignet API: http://localhost:58089
- Existing Certify: http://localhost:58090
- Existing Inji Web: http://localhost:58093

Provider authentication material is stored in ignored files under `infra/acceptance/private` and the corresponding `.env.*` files. No secret values belong in validation logs.

## Observed local results, 2026-09-06

Four fresh development identities completed the official eSignet browser PIN and consent flow. CREST established the operator through its authenticated one-time setup API and refused a replay. The other actors registered themselves through the public party API. Project creation, scoped role grants, independent definition approval, worker roster joining and screen consent all ran through public APIs.

A deliberately retained failed intake exposed an invalid evidence schema reference. The row entered the unclear queue; an independently approved definition version 2 with the canonical evidence schema produced a claim. Unknown schema references are now refused before publication. Source registration now requires the source-owner permission, and authorization checks reject nonexistent project contexts.

The claim generated a notification in the HTTP inbox. A different actor could not acknowledge it; the worker could. After the configured two-minute wall-clock review period, CREST issued a signed credential. Online verification succeeded, and the browser verified that actual credential offline without API requests while showing that withdrawal status was not checked. The worker UI displayed the credential and exported an encrypted history backup. Repeating the same CSV produced no additional claim.

Payments and notification persistence/idempotency contracts also passed against isolated PostgreSQL test schemas. Those tests are distinct from the live actor journey. The notification transport probe is labeled as such and never represents worker reach.

Existing companion containers needed repairs: Certify was rebuilt with the signed CREST plugin and connected to the clean core. Mimoto's keystore lacked aliases required by its database; a separate repaired volume restored the Inji issuer endpoints. The original `crest_mimotocerts` volume is preserved. Replacement keys do not recover anything encrypted under missing original keys; the inspected Mimoto database contained only key-management tables and its data directory was empty. Authenticated companion retrieval and the live payment journey are tracked separately in the run record.

## Reproducible companion run

The repaired Mimoto deployment is reproducible with the ignored, mode-600 environment file and the tracked external-volume overlay. The file contains the locally registered client and its private provider settings; the current PKCS12 is already in `crest_mimotocerts-rotated-20260906`, while the original volume remains untouched.

```sh
docker compose --project-name crest --profile full \
  --env-file infra/acceptance/.env.mimoto \
  -f infra/compose/docker-compose.yml \
  -f infra/acceptance/companions.mimoto-clean.override.yml \
  -f infra/acceptance/companions.did-tls.override.yml \
  build mimoto
docker compose --project-name crest --profile full \
  --env-file infra/acceptance/.env.mimoto \
  -f infra/compose/docker-compose.yml \
  -f infra/acceptance/companions.mimoto-clean.override.yml \
  -f infra/acceptance/companions.did-tls.override.yml \
  up -d --no-deps mimoto inji-web
docker compose --project-name crest --profile full \
  --env-file infra/acceptance/.env.mimoto \
  -f infra/compose/docker-compose.yml \
  -f infra/acceptance/companions.mimoto-clean.override.yml \
  -f infra/acceptance/companions.did-tls.override.yml \
  up -d --no-deps --force-recreate mimoto-localhost-esignet mimoto-localhost-certify
docker compose --project-name crest --profile substrate \
  --env-file infra/acceptance/.env.certify \
  -f infra/compose/docker-compose.yml \
  -f infra/acceptance/companions.override.yml \
  -f infra/acceptance/companions.did-tls.override.yml \
  up -d --no-deps inji-certify certify-did-tls inji-verify-service
```

Mimoto reads its issuer/client configuration from Inji Web, so both services must use the same environment file. The two network relays must be recreated after Mimoto changes, because they share its container network namespace. Mimoto also needs `MOSIP_SECURITY_ORIGINS=http://localhost:58093` for the browser download POST.

Independent browser validation completed eSignet PIN login and authorization-code exchange for the actual worker. A separate supported Certify issuance request returned HTTP 200 and an `Ed25519Signature2020` credential for a second claim created through CSV submission, worker acknowledgement and worker confirmation. Its private response is `infra/acceptance/private/actual-certify-response.json`. This is distinct from the earlier core credential; the earlier agent's unsupported Certify success claim was withdrawn.

The companion DID failure is repaired with `did:web:certify.crest.test:v1:certify:.well-known`, served by a private Docker-network HTTPS proxy. Certify's environment and stored credential configuration now use the same DID. The local CA is trusted only by the Mimoto and Inji Verify JVM truststores, which retain their default public roots. No host HTTPS port or machine-wide trust was changed. The local server certificate expires after 90 days and requires renewal; these are development settings, not public production DID hosting.

A fresh, genuine Certify credential returned `SUCCESS` through native Inji Verify. Altering its actual activity returned `INVALID`. Mimoto also passed credential verification. Its missing PDF template has been added and an actual worker download succeeded. The PixelPass QR extracted from that downloaded PDF matched the actual CSV claim and returned native Inji Verify `SUCCESS`. The source-level Mimoto rendering fix is now built and running. The final actual PDF contains activity, period start, outcome value/unit, source class, capture method and receivedAt, with no unrendered template expressions. Its extracted PixelPass QR matches the actual CSV claim and returns native Inji Verify `SUCCESS`; altering the embedded period returns `INVALID`. The patch preserves the signed credential and QR payload. The core's own `DataIntegrityProof` format and offline wallet checks remain distinct from Certify's `Ed25519Signature2020` path.

The local Inji Web and DID reverse proxies now resolve Docker service addresses at runtime; replacing a backend no longer leaves nginx pinned to its old container IP.

The payment journey exercised the missing-rate hold, authorized rate publication, public retry, pending simulator submission, explicit authorized settlement and idempotent replay. Reconciliation reported no gaps and the worker statement recorded 100 KES of simulated compensation. The rate was published later with an explicit effective-from date covering the work date; the instruction retained its selected immutable rate record/version and pricing time. This does not represent a real bank transfer.

The worker wallet subsequently saved encrypted history, acknowledged custody, and successfully unlocked the actual credential after a logged-out offline reload. The core credential envelope retained metadata and no signed document. The encrypted export also restored in a fresh offline browser with no backend calls.

## Recovery and remaining gates

A quiesced backup was restored into a separate project. Supported API reads verified the actual project, published definition, notification, confirmed simulated payment, issuer continuity and credential custody metadata/digest. The recovery project is stopped with its volumes preserved. The restore rehearsal used `/tmp/crest-development-recovery-20260906`. A newer quiesced core backup, `/tmp/crest-development-current-20260906`, includes the subsequent credential rotation and second claim and passes manifest/archive verification. That newer bundle has not been restored again. MOSIP companion database/keystore recovery is a separate unproven gate; the core recovery result does not cover those volumes.

A diagnostic exposed three development database-role passwords and Mimoto client key material. The affected passwords were rotated in both primary and restored databases. The exposed eSignet client was deactivated and replaced with a freshly registered RSA client and separate keystore volume. Primary services were restarted with the replacement credentials. Historical private bundles remain sensitive and are not current deployment credentials.

Before merge/deployment, close the companion lifecycle gates below and rehearse companion database/keystore recovery. Broader reference/deployment gates still include assisted/printed custody retention, signed offline federation/status freshness, a migration principal separate from runtime database roles, legacy lifecycle upgrade rehearsal, and production TLS/alert/recovery operation. Production identity, SMS, real payment providers and non-CSV evidence adapters remain deferred as agreed. No merge or deployment has been performed.

## Companion recovery acceptance

The acceptance-core bundle does not include the shared companion PostgreSQL databases (`inji_certify`, `inji_mimoto`, `inji_verify`, `mosip_esignet`, `mosip_mockidentitysystem`) or the companion signing/storage volumes. Rehearse recovery of that database state together with `crest_certifykeys`, `crest_certifydata`, `crest_esignetkeys`, `crest_mockidentitykeys`, and the active `crest_mimotocerts-rotated-20260906`, plus the matching private client, TLS, issuer and deployment configuration. Preserve older keystore volumes when historical encrypted data depends on them.

Use an isolated project/network and protect the original volumes. Restore database and keystore snapshots from the same quiesced point. Preserve issuer/DID identity and signing keys; simply changing public issuer URLs to avoid port conflicts is not an identity-continuity test. Validate actual eSignet login/JWKS, retrieval and verification of a pre-backup credential, actual Inji PDF/QR verification, and tampered-credential rejection. Include any object-store, DeDi, session or anonymous-volume dependencies that the selected restored routes actually use. The present core restore result does not satisfy this gate.

## Companion lifecycle implementation gates

The current Certify integration issues a separate credential from the newest available work event. Signature verification succeeds, but there is no persisted link between its lifecycle and the underlying CREST credential. Core revocation prevents future collection of that record; it does not update an already-issued Certify credential. The collector also cannot reconstruct a credential whose central document was removed by worker custody transfer. These are implementation gaps, not missing production provider credentials.

1. Persist a verifiable association between the core credential/claim and every companion issuance. Carry the core record ID/digest and lifecycle identity through the authenticated issuance exchange; retain only the minimum association required by custody/privacy rules.
2. Emit durable core lifecycle events and implement a companion adapter that applies them through Certify's supported status APIs. Require authenticated service calls, idempotent retries, an outage/reconciliation queue and observable failed deliveries. Validate actual issuance, core revocation, propagation and rejection/status reporting by each selected online verifier. Signature success alone is insufficient. Keep consent withdrawal semantics separate until reconciled with the reference requirements; do not automatically erase valid historical work.
3. Add an authenticated, worker-owned event selector to the issuance contract. Test two owned events and an unauthorized other-worker event. The present hardcoded newest-of-20 behavior does not satisfy historical selection.
4. Specify and implement the post-custody companion route using worker-held material or a reference-compliant minimal record. Prove encrypted wallet export/import and permitted companion retrieval after transfer without restoring forbidden central credential copies or manufacturing replacement facts.
5. Re-run the full worker journey and companion recovery rehearsal with these lifecycle checks before claiming complete companion acceptance or deploying this integration as complete.

## Mimoto rendering build

The rendering correction is a source patch against upstream Mimoto v0.19.2, commit `e1fedbb9dce6a8c3dd9b5b8e75423d636a0704cf`. `Dockerfile.mimoto` verifies the source revision, applies the patch and runs normal Maven package/tests before copying the rebuilt JAR into the runtime image. The patch changes human-readable display formatting only: nested fields retain their labels, outcomes retain their units, and the original signed credential still supplies the QR. The template separately escapes rendered text. Future Mimoto upgrades must rebase or retire this patch and rerun actual PDF-field and QR verification checks; passing the core build does not validate the companion image.

The companion browser acceptance here is Inji Web's guest download, backed by actual eSignet worker login, Certify and native Inji Verify. It does not establish mobile-device acceptance or Mimoto account-based cloud wallet/background WebSub/authmanager integration. CREST's encrypted offline worker wallet was validated separately.

Final local status: the selected development journeys pass, including the rebuilt Mimoto PDF/QR path. The finalized companion image passed its normal Maven package/test build; the local upstream test reports total 409 passing tests. The local services remain running. Lifecycle, historical/custody retrieval and companion recovery gates above remain open; no merge or deployment has been performed.
