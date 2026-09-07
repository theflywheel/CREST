> Scope update, 2026-09-06: the current development run uses official eSignet with its development identity plugin, CSV evidence, a durable HTTP notification inbox, and the separate payments service with an explicit simulator. Production credentials and non-CSV adapters are deferred by the user; they are not blockers for this development run. See [DEVELOPMENT_VALIDATION_PLAN.md](DEVELOPMENT_VALIDATION_PLAN.md) and `infra/acceptance/validation-status.json` for current execution status. Earlier entries below are historical findings.

**CREST infrastructure completion plan**

Status: implementation started; no completion or local acceptance claim yet.

**Authority and scope**

The three documents in `docs/reference` remain the only requirements authority. The fresh implementation review identifies candidate defects; code and acceptance evidence determine closure. Other documentation, generated fidelity ledgers, code comments, and existing passing tests cannot waive a reference requirement. Existing working-tree edits are preserved.

Scope covers the infrastructure contracts behind the actor journeys: identity and authorization, registries/versioning, source provenance, evidence/units/claims, consent, attestation and corrections, worker custody/disclosure, public verification, reliable optional payment handoff, and deployable operation. Product work is included where needed to exercise those contracts through actual onboarding and use.

Merge and deployment follow successful local acceptance and review. They are not part of the current local execution stage.

**Non-negotiable validation rules**

- Use a new isolated local Compose project, fresh database/object-store volumes, new signing keys, and normal wall time. Preserve the existing running stack and its data.
- Do not run story/fixture/demo/seed scripts, bootstrap SQL inserts, the existing bootstrap-operator script, prebound identity scripts, clock jumps, or privileged internal calls to fabricate a successful journey.
- Configure the trust root as a deployment operation, then establish its actual organization/administrator through a one-time authenticated setup API. This setup must be auditable, fail after initialization, and reject unrelated identities. It is part of the implementation, not a testing shortcut.
- All subsequent organizations, terms, projects, workers, definitions, source connections, approvals, consent, claims, and disclosures must be created through supported authenticated application/API workflows. IDs come from responses; never from fixture constants.
- Browser/API validation automation is allowed only as an ordinary client driving those workflows. It must assert intermediate and final state and never write business data directly to a database.
- Unit tests may isolate dependencies and use test doubles. A double passing is not evidence that a real identity, notification, source, or payment integration works. Record each external integration as genuinely validated or explicitly outstanding.
- Missing configuration, failed setup, and unavailable integrations must fail visibly. Do not add success fallbacks, fabricated receipts, silent empty results, blanket security exceptions, or skipped failing cases.
- Schema changes require explicit migrations and historical-data handling. Never delete existing data or rewrite a signed credential merely to make a test pass.

**Implementation decisions and requirements**

1. Tier 1 is strongest; Tier 3 is worker asserted. The public result is derived at query time. Existing definitions/assessments using reversed numbering need an explicit semantics version and migration policy; immutable signed historical documents cannot be silently rewritten.
2. A unit identifies work that happened independently of any worker claim. Source event identity and definition version must be explicit. Unknown attribution does not erase valid work. Multiple claims must not multiply delivered output.
3. Source facts are established by registered, authorized integration configuration, not uploaded assertions. Configured schema and evidence rules govern both preview and acceptance.
4. Protected operations always require a verified actor and the relevant function/scope. Authenticated service calls are distinct from end-user callers; private networking alone is not authorization.
5. Collection requires affirmative scoped consent; assisted consent is represented truthfully and includes actual evidence when declared as voice. Disclosure is per approved request. Anonymous holder-supplied cryptographic checking is distinct from retrieving private records.
6. Credential lifecycle belongs to core infrastructure. Payment is an optional subscriber; a project with no payment feature still produces credentials. A payment adapter never claims settlement from an HTTP acknowledgement.
7. Worker-owned durable encrypted custody and full-history transfer must replace dependence on the central credential inventory. Issuer retention is restricted to the agreed lifecycle/audit/status facts. Any necessary transitional migration is named explicitly and is not treated as final compliance.
8. Public verification resolves the issuing instance's trusted historical key, definition, and status evidence. Offline results state what was established and the age of cached status; they cannot assert current revocation freshness without a connection.
9. Organization admission initially uses authenticated self-registration with operator approval and explicit project grants. Delegated organizational accreditation remains a separate reference decision; the code must not pretend a distributed chain exists.

**Work packages and closure gates**

| Package | Review coverage | Implementation | Required acceptance evidence |
|---|---|---|---|
| A: Identity and authorization | 1–3, 19, 21 | Strict end-user and service authentication; protected route inventory; real actors; scoped reads/mutations; genuine first-run operator setup | Anonymous, worker, unrelated organization, and unrelated project each rejected from protected operations; no legacy route bypass; bootstrap replay rejected |
| B: Evidence contract | 4–6, 15–16, 18, versioning | One evaluator; registered source facts; exact definition/schema version; raw-PII rejection; independent units/claims; correct tier semantics | Preview/live parity; source spoof rejected; no-consent rejected; old version honored; two workers/one source event yields one unit/two claims; unknown worker retains valid unit |
| C: Registry lifecycle | 1, versioning | Genuine separate author/approver; immutable versions; append-only payment definitions; function/project grant enforcement | Author cannot approve own version via any endpoint; superseded definition remains resolvable; historical payment setup cannot be overwritten |
| D: Review and correction | 7, 14, 17 | Actual notification delivery and acknowledgement; honest assisted consent; review state machine; assigned dispute decision/correction/withdrawal; recovery | Worker receives and can act on draft; silence not treated as proven reach; unauthorized decisions rejected; dispute closes with reviewer/outcome; correction preserves history |
| E: Optional payments | 8–10 | Core acceptance independent of payments; idempotent subscription; real rail status and actual settlement amount; asynchronous settlement and recoverable holds | Credential-only project works; pending != paid; failed/partial/duplicate settlement handled; outage recovery produces one instruction; no indefinite silent hold |
| F: Custody and verification | 11–13, 19 | Encrypted durable wallet; complete history transfer/import; consented disclosure; issuer/definition/status resolution; offline trust cache; key rotation | Worker keeps all history after application outage; separate instance verifies; key rotation preserves old credentials; offline freshness explicit; only approved scope disclosed |
| G: Field client integrity | 12, 14, 20 | Real voice capture; encrypted durable queue; original actor/project/operation identity; resume after partial completion; usable offline app | Offline registration survives restart and project switch; storage failure visible; actual recording replayable; duplicate sync idempotent |
| H: Sources and operational deployment | 18, 21–22 | Real class connectors and retrieval workers; no mock provider in acceptance profile; production secret checks; wall-time scheduler; revision/dependency readiness; restore path | Fresh full stack starts from documented configuration; real external integrations exercised; outage/restart recovery; backup restores and verifies historical records |

Every package needs code, meaningful tests, integration review, and local evidence. A package is not complete because a route or screen exists.

**Execution waves and agent ownership**

Wave 1 runs three bounded smaller-model agents (gpt-5.6-luna) with disjoint ownership: definitions authorization/lifecycle, payment integrity/authorization, and evidence/provenance/privacy. The coordinating agent owns shared identity/client/strength contracts, schemas/code generation, verification, the first-run path, integration review, and this ledger. Shared-contract changes are agreed before consumers use them; agents do not edit another owner's files.

Wave 2 completes shared contracts and migrations, reviews each first-wave patch, and exercises negative integration tests. Smaller-model agents are then assigned notification/dispute lifecycle, durable field consent/queue, and portable wallet/public verification as bounded tasks. The coordinator integrates core/payment separation and remaining schema changes.

Wave 3 implements real connectors and the complete isolated local deployment profile, removes demo assumptions from application paths, and adds clean-start client validation. A provider requiring external credentials is reported as pending rather than replaced by a mock without agreement.

Wave 4 starts the full clean stack and executes the matrix below. Failures return to implementation. Only after all required gates pass do we prepare the merge/deployment review.

**Clean-start local acceptance sequence**

1. Inspect available resources, existing containers, provider configurations, and image availability without exposing secrets. Create separate ports, project names, and volumes. There is an existing full stack; it is not clean-start acceptance evidence.
2. Build all changed services/apps/companions, run migrations on empty stores, verify the exact revision and dependency readiness. Generate private keys through deployment setup, not source defaults.
3. Authenticate the intended root administrator through the configured actual identity provider and run first-time setup through its supported API/UI. Confirm that the same operation cannot be replayed or used by another subject.
4. Register two delivery organizations plus payment/verifying organizations where applicable. Have the operator approve explicit terms. Create two projects and establish scoped administrators, independent authors/approvers, source operators, support/reviewers, and verification permissions through invitations/acceptance. Test cross-project denials before doing work.
5. Register a self-service worker and an assisted worker with no phone/document. Capture genuine consent, prove refusal/withdrawal behavior, and test a probable duplicate without silently merging it.
6. Author, dry-run, independently approve, and activate a definition. Register the real evidence source and mapping. Create a second version; prove that earlier work names and resolves the correct version.
7. Send actual source exports/events through the supported integration. Include valid, malformed, unknown-worker, shared-unit, unauthorized-source, no-consent, prohibited-PII, and replayed records. Observe exact dispositions; no direct inserts or forged provenance.
8. Deliver the notification, confirm one claim, dispute another, and complete an authorized assisted route. Exercise the automated review path using a deliberately short supported local project configuration and elapsed wall time, with the same code as the normal duration. Do not advance system clocks or call an internal finish endpoint.
9. Issue and transfer all relevant credentials to the worker. Run a credential-only project with payment disabled. Present approved scope to a different verifier, reject unauthorized retrieval, and check audit attribution.
10. For the payment-enabled project, use the configured rail sandbox, distinguish acceptance from settlement, and reconcile actual amounts. Exercise pending, failure, retries, and duplicate delivery. Preserve any external-provider limitations in the evidence.
11. Complete dispute resolution/correction, verify withdrawn/replacement behavior, rotate keys, and retain historical validity. Test full-history custody after the original application is stopped.
12. Disconnect the field/worker/verifier clients and exercise their actual offline flows. Reconnect and verify durable, correctly scoped, idempotent synchronization and status freshness.
13. Restart services during in-flight operations, simulate dependency loss, recover holds/outboxes, and restore an isolated backup. Verify public historical credentials and private access boundaries after recovery.
14. Produce a machine-readable run record plus a human review containing commands, revision, real/simulated provider boundaries, assertions, failures/retests, and outstanding decisions. Local completion requires no unexplained failure or seed-dependent success.

**Current execution ledger — 2026-09-06**

Implementation uses three bounded `gpt-5.6-luna` agents with coordinator integration and independent follow-up review. Existing user edits and the running `crest` project have been preserved. Work is uncommitted; no merge or deployment has occurred.

| Area | Implemented in this worktree | Closure still required |
|---|---|---|
| Authorization and governance | Verified actors required in local mode too; scoped definition/source/finance/registry operations; independent definition governance; immutable published versions; authenticated one-time root setup | Real administrator/organization/invitation flows and cross-project browser denial tests |
| Service infrastructure | Per-service Ed25519 signatures with route permissions, body binding, durable replay claims; no redirect credential leakage; bounded previous-key rotation; separate core/payment DB roles and runtime secrets; protected outbox metrics | Production deployment configuration, rollover rehearsal, external-provider readiness and operational alerts |
| Evidence | Registered source provenance, exact active definition version, canonical validation, reference tier numbering with explicit historical semantics, private reads, source silence delivery retries | Real source-class connector contracts/retrieval implementations beyond CSV; external source acceptance |
| Credential lifecycle | Review/notification/contest lifecycle moved to core; payment subscriber independent; real notification transport, acknowledgement-started windows, durable correction decisions | Full notification/dispute/correction acceptance; approved interpretation of outstanding correction semantics; legacy-window migration rehearsal |
| Payments | Work-date rate selection with immutable rate linkage, retry preservation, rail identity and actual settled amounts | Real rail sandbox contract and pending/failure/partial settlement/reconciliation acceptance |
| Custody and verification | Encrypted worker history export/import and explicit custody acknowledgement; scoped private APIs; trusted federation/history/status validation; actual offline signature checks with an expiring issuer-key cache | Assisted/printed/no-phone custody handoff; signed offline federation/status snapshots; independent-instance and historical key acceptance |
| Field client | Real voice recording, encrypted durable IndexedDB queue/audio, stable operation keys and partial-sync checkpoints | Actual device offline/restart/storage-failure/reconnect tests and genuine recorded consent |
| Data and recovery | Serialized migrations/checksums, fenced outbox leases, consent upload-intent recovery and withdrawn-recording deletion retry; quiesced backup/restore tooling | Recovery with actual historical business records, ambiguous-commit/outage rehearsal across services, deployed-history checksum baseline review |

Local evidence so far:

- Go test suite and `go vet ./...` pass. Database-only tests run separately against isolated random schemas on the fresh acceptance PostgreSQL instance; test fixtures are not acceptance business data.
- All six service migration sets apply and replay against empty schemas. Real database tests cover concurrent migrations/adoption, edited migration rejection, outbox leases, durable nonce races/expiry, and consent upload recovery.
- Frontend typecheck/build and the Java Certify plugin build pass. Offline signature tests verify a Go-signed credential with real Web Crypto and reject tampering, stale trust, invalid purpose/times, oversized documents and unsafe numeric inputs. Offline checking makes no network calls and does not claim current withdrawal status. Worker wallet unlock/import remain available during a backend outage and without a login session; full signed documents survive list/detail/show navigation.
- Core and payment Docker images build. Both final containers reject the missing OIDC issuer as required. Both Railway nginx templates pass `nginx -t`; production mock routing was removed, confirmation routes now target core, and proxy request limits were added. The acceptance static app routes serve current built assets; internal and mock routes return 404. Field, verifier and worker app shells pass cold offline reloads in fresh browser contexts. A logged-out worker reaches the local passphrase unlock screen without any backend call. These are shell checks, not authenticated actor acceptance.
- Dedicated CI now runs Go/vet/database migration contracts, frontend typecheck/build/offline cryptographic tests and the Certify plugin build. Workflow syntax is checked locally; GitHub CI has not run.
- Separate `crest-acceptance` PostgreSQL, object storage, persistent Vault and DeDi dependencies run on isolated volumes/ports. Private deployment files are ignored by Git and Docker build context. No business rows have been created through seeds, bootstrap scripts or direct inserts.
- Quiesced backup restored all four volumes plus private deployment configuration into `crest-recovery-check`. Restored PostgreSQL roles match; Vault retains the exact issuer secret and scoped-token access; restored DeDi health passes. Recovery containers were stopped after the drill, retaining volumes. This is an infrastructure-only drill, not historical-credential recovery acceptance.
- Signed S3 readiness initially found an absent bucket on the fresh stack. `tools/provision-objectstore` now provides an explicit idempotent infrastructure provisioning operation; the acceptance bucket was provisioned and authenticated readiness passed. No business objects were created. The earlier backup predates this bucket provisioning.

**Open gates — do not merge or deploy yet**

1. Supply real identity/eSignet, notification and payment sandbox configuration through private configuration files. Preflight and application startup must continue refusing incomplete or mock-backed acceptance configuration. Identity configuration includes the actual browser authorization/callback client, not just token verification endpoints.
2. Complete the source-class connector implementations against the selected real provider contracts. CSV validation does not establish that institutional APIs, polls, webhooks or provider reconciliation work.
3. Close assisted custody and complete the signed offline federation/status snapshot contract. An encrypted self-service wallet does not prove no-phone custody or offline withdrawal freshness.
4. Rehearse the upgrade path: stop the old payment window writer before moving its lifecycle data into core; exercise the implemented open-window token/re-notification adoption against actual legacy records; establish checksum provenance for previously unchecksummed migrations; explicitly retain historical issuer keys.
5. Configure and exercise required MOSIP companions (or the explicitly selected equivalent deployment path). Building the Certify plugin alone is not OpenID4VCI/eSignet/Inji integration acceptance.
6. Run the complete clean-start sequence above with actual actors, supported APIs/UI, real elapsed review time, genuine source records and real provider responses. Capture revision, assertions, actual provider boundaries, failures and retries in the run record.
7. Complete production operational configuration: TLS/routing, secret distribution/rotation, least-privilege identities, dependency/outbox/source alerts, resource and availability policy, encrypted off-host backups and restore objectives. The local Compose profile does not establish these production guarantees.

Only after these gates pass: review the full diff, prepare the merge, then deploy and run post-deployment verification. No claim of an “iron clad” system is justified by unit tests or infrastructure health alone. The machine-readable local evidence record is `infra/acceptance/validation-status.json`.

**Service key rotation grace**

`CREST_SERVICE_PEERS_JSON` may keep the existing `publicKey` as the current
key and list bounded rollover keys as `previousKeys`, each with an explicit
RFC3339 `notAfter`. A previous key is accepted only through that expiry; an
expired or malformed previous entry is never treated as trusted. The service's
private key must match the current key or an unexpired previous key, which
allows a staged restart without accepting unlimited historical keys.

Roll a key in three deployment steps: add the new public key as a previous key
with a short, agreed expiry; deploy that trust configuration while the old
private key is still signing; switch the service to the new current public key
and private key; then, after every peer has passed the grace deadline, remove
the old previous entry and restart. Each peer must trust the new key before its
sender switches. Configuration is read at startup, so a restart is required;
there is no hot reload or production rotation operation in this repository.
