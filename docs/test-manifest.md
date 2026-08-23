# Test manifest

Every feature, and how we know it works. Maintained **by hand** — see [TESTING.md](TESTING.md) for why.

**Status values:** `planned` (no code yet) · `partial` (some layers proven) · `spike` (a check exists and passes, run on demand rather than in CI) · `covered` (every listed check passes in CI) · `unproven` (code exists, validation does not — treat as a bug).

**How to use this file**
- Adding a feature? Add its row in the same change. A feature with no row is unproven.
- Closing an issue? Its manifest rows must be `covered`, or the issue says explicitly why not.
- Reviewing? Check the row, not the diff's optimism.

---

## W1–W10 — worker invariants

These are the promises to the worker. They must be executable at G3 (#33); until then they are `planned` and that is a debt with a date on it. Blueprint §11.

| ID | Invariant | How it is proven | Layer | Status |
|---|---|---|---|---|
| W1 | Work recorded is work that happened | Evidence with no source record is rejected; harness asserts no unit exists without a canonical record | E2E | planned |
| W2 | A worker sees what was recorded about them, before it counts | Confirmation notification asserted for every draft claim; no claim reaches ACTIVE unnotified | E2E | planned |
| W3 | Silence is not consent against the worker | Auto-confirm at T=7 still releases payment **and** stays disputable afterwards | Unit + E2E | planned |
| W4 | A dispute never costs the worker their money | All four T=7 exits assert payment release; dispute path asserted explicitly | Unit + E2E | planned |
| W5 | A disputed claim never destroys the underlying unit | Unit survives claim contest; re-claim possible by another party | Unit | planned |
| W6 | A worker's record is portable and verifiable without CREST | Printed card verifies offline with all CREST services stopped | E2E | planned |
| W7 | No disclosure without consent, per request | Verification without consent artefact rejected; refusal recorded as a value, not an error | E2E | planned |
| W8 | A worker can see who checked them | Check audit trail contains every verification, including refused ones | E2E | planned |
| W9 | Identity is never over-collected | Assert no raw national ID or biometric in any store; only pairwise ref + salted hash | Contract + E2E | planned |
| W10 | A held payment always has a reason with an owner | Every reconciliation gap has owner + reason; the cleared-but-never-instructed deadline rule fires | E2E | planned |

**The W9 check runs against the actual datastore**, scanning for anything resembling a raw identifier. An invariant about what is *absent* cannot be proven by testing the happy path.

---

## Foundations

The scaffolding. These are `covered` because the code exists and the checks run — the only rows in this file that are.

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| Injectable clock | — | Advance 7 days in microseconds; backwards travel; concurrent read/advance under `-race`; System clock is UTC | Unit | covered |
| Only `pkg/clock` reads wall time | — | `forbidigo` rule; verified by a deliberate violation failing lint | Lint | covered |
| Services cannot import each other | — | `depguard` rule in `.golangci.yml` | Lint | covered |
| DB drivers confined to `pkg/store` | — | `depguard` rule; verified by a deliberate violation failing lint | Lint | covered |
| Repository layout | — | `make structure`; verified by a stray directory and an unknown service both failing | Lint | covered |
| Health and readiness endpoints | — | Both respond per service; health reports the **injected** clock, so a service silently reading wall time is caught | Unit | covered |
| Service images build and run | — | Distroless image builds; container starts; `/healthz` answers over HTTP | E2E | covered |
| Per-service Postgres schemas | — | All seven schemas created on first boot; no cross-schema foreign keys | E2E | covered |
| Mock rail enforces idempotency | — | Same key twice → one instruction, replay header on the second | Contract | covered |
| Mock rail injects real failure modes | — | `timeout` mode: caller sees 504 while the rail recorded `cleared` — the cleared-but-never-confirmed case W10 must survive | Contract | covered |
| Mock SMS records messages | — | Sent messages readable per recipient, so "the worker was notified" (W2) is assertable without a phone | Contract | covered |

## P0 spikes

Spikes prove things about **other people's software**, so they live outside the harness and are marked `spike` rather than `covered` — they are run deliberately, not in CI, and they retire when the thing they de-risked is wrapped by CREST code. Findings: [p0-findings.md](p0-findings.md).

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| DeDi publish → inclusion proof | [#2](../../issues/2) | `make spike-dedi`: record published, fetched with `?proof=inclusion` | Spike | spike |
| Inclusion proof validates independently | [#2](../../issues/2) | `tools/spikes/dediproof` — a second implementation, stdlib only. Proven by **tampering**: altered leaf digest, rewritten `created_by`, flipped audit-path step and wrong verifier key are each rejected | Spike | spike |
| Definition versions are immutable | [#2](../../issues/2) | v2 published; v1 still resolves by `version_id` **and its original proof still validates** — a credential issued under v1 stays verifiable | Spike | spike |
| Upstream image tags exist and match our platform | [#1](../../issues/1), [#3](../../issues/3) | Every tag in `.env.example` checked against the registry and pulled. Found `inji-verify:0.11.0` does not exist and the component is two images | Spike | spike |
| Registry spike against the **deployed** node | [#2](../../issues/2) | `make spike-dedi-deployed` — same script, real node, public origin. Caught a proof-checker bug that every local run had passed | Spike | spike |
| `WorkEventCredential` issued over OpenID4VCI | [#1](../../issues/1) | `make certify-issue` — against the **deployed** Certify: authorization-code flow through eSignet, `openid4vci-proof+jwt` holder proof, credential returned. Nine assertions | Spike | spike |
| The issued credential carries provenance, not a tier | [#1](../../issues/1) | `make certify-issue` — asserts `sourceClass, captureMethod, adapterRef, receivedAt, sourceExposure` all present, and that no `tier`, `trustTier`, `nationalId`, `uin`, `individualId`, `face` or biometric field appears anywhere in it (W9; "trust strength is derived, never stored") | Spike | spike |
| The credential's proof verifies against the issuer's published key | [#1](../../issues/1) | `make certify-issue` — `eddsa-jcs-2022` re-verified independently against the `did:web` document Certify itself serves, with RFC 8785 canonicalisation | Spike | spike |
| The credential is bound to the holder's own key | [#1](../../issues/1) | `make certify-issue` — `credentialSubject.id` is the `did:jwk` of the key that signed the proof-of-possession | Spike | spike |
| The wallet is offered the credential | [#1](../../issues/1) | `/v1/mimoto/issuers/CREST/configuration` on the deployed Mimoto returns `WorkEventCredential` — which requires Certify's well-known **and** eSignet's authorization-server metadata to both resolve (findings C11, C12) | Spike | spike |
| Credential in a wallet → printed card → offline verify | [#1](../../issues/1) | **Not proven.** Issuance and the wallet's offer are; the browser download, the PixelPass card and the offline leg are not. The offline leg needs a real device with radios off | E2E | planned |
| eSignet deployed and answering discovery | [#3](../../issues/3) | `make verify-deployed` — discovery is served over the public URL and `issuer` matches the host it was fetched from | Spike | spike |
| eSignet advertises only pairwise subjects | [#3](../../issues/3) | `subject_types_supported == ["pairwise"]` in the discovery document. **Configuration, not behaviour** — the rows below are the behaviour | Spike | spike |
| eSignet subject is stable across sessions | [#3](../../issues/3) | `make spike-esignet` — two full authorization-code round-trips per client, same `sub` both times | Spike | spike |
| eSignet subject is partitioned by relying party | [#3](../../issues/3) | `make spike-esignet` — a client under a different `relyingPartyId` receives a different `sub`; two clients under the same one receive the same `sub` (finding E6) | Spike | spike |
| The id_token carries no national identifier | [#3](../../issues/3) | `make spike-esignet` — decoded id_token claims are `sub, aud, acr, auth_time, iss, exp, iat, nonce, at_hash`; `individual_id` is never requested (W9) | Spike | spike |
| eSignet relying-party registration in a real deployment | [#53](../../issues/53) | Not a test, and not P0. A partner conversation, blocked on choosing a pilot geography; the mobile-OTP provider class is the stated fallback | — | planned |
| `individual_id` is never requested or stored | [#3](../../issues/3) | The claim is offered (finding E2). Proven when the relying-party registration exists and a test asserts the requested claim set excludes it (W9) | Contract | planned |

## Deployment

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| Deployed stack answers | — | `make verify-deployed`; deploy workflow polls `/readyz` and fails if it never answers | Deploy | covered |
| Deployed log is independently verifiable | — | `make verify-deployed` validates a real inclusion proof with our own verifier, against a key cross-checked against the checkpoint signature | Deploy | covered |
| `main` deploys only after CI passes | — | `deploy.yml` triggers on CI **conclusion == success**, not on push | Deploy | covered |
| Commit messages are machine-checked | — | `commitlint` in a lefthook `commit-msg` hook; unknown scopes and non-conventional subjects rejected | Lint | covered |
| Secrets cannot reach a commit | — | `guard-secrets.py --scan` in the lefthook `pre-commit` hook, sharing its pattern table with the agent hook | Lint | covered |
| Rollback | — | **Not proven.** Railway redeploy exists; no tested path back to a previous version | Deploy | planned |
| Backup and restore | — | **Not proven, and not configured by us.** A backup nobody has restored from is a belief | Deploy | planned |

## Infrastructure layer

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| Eleven primitive schemas | [#12](../../issues/12) | `make test-contract` — every schema compiles and is reachable by `$id`; the fixture world validates against all of them at load | Contract | partial |
| Go types cannot drift from the schemas | [#46](../../issues/46) | `make generate-check` in CI — `tools/codegen` regenerates `pkg/schema` and fails if the committed file differs | Lint | covered |
| The canonical fixture world | [#40](../../issues/40) | `make test-contract` — `tests/fixtures/world.yaml` loads, every object validates, and every named id resolves | Contract | covered |
| Trusted Payments profile | [#13](../../issues/13) | The three LinkedRecord payload schemas exist and validate, and no primitive schema mentions payments. Profile-needs-no-primitive-change not yet asserted mechanically | Contract | partial |
| Canonical evidence record | [#14](../../issues/14) | `tests/contract/adapter_test.go` — every record the CSV adapter emits validates against `schema.IDEvidenceRecord`, and a record with only the mandatory core is valid | Contract | covered |
| Provenance is adapter-attached, never source-asserted | [#14](../../issues/14) | `tests/contract/adapter_test.go` against `csv-asserting-its-own-provenance.csv`: a file's own `source_class`/`capture_method`/`adapter_ref` columns are ignored and kept as enrichment (§8) | Contract | covered |
| A batch loses no rows | [#22](../../issues/22) | `adapters/csv/csv_test.go` — every data row leaves as a record or a named rejection, and the reference points at the real line even after a quoted newline | Unit | covered |
| CSV batch adapter | [#22](../../issues/22) | `adapters/csv/csv_test.go` — six bad-value cases reject one row each; a missing mandatory column fails the file and names it | Unit | covered |
| Source heartbeat / silence detection | [#14](../../issues/14) | Harness stops a source; asserts a silence alert fires rather than nothing happening | E2E | planned |
| Strength function `f` | [#15](../../issues/15) | `pkg/strength/testdata/vectors.json` — nine cases run as a table, readable by a second implementation in any language | Unit | covered |
| Transport never affects strength | [#15](../../issues/15) | One set of facts through all four transports must give one answer (§8) | Unit | covered |
| A credential resolves against its pinned definition version | [#15](../../issues/15) | The same facts under v1 and a stricter v2 give different tiers, and v1 does not move | Unit | covered |
| A re-assessed source downgrades without reissuance | [#15](../../issues/15), [#6](../../issues/6) | A `SourceAssessment` cap changes the tier for an unchanged credential, and the reasoning says so | Unit | covered |
| WorkEventCredential issuance | [#16](../../issues/16) | `make test-e2e` — a confirmed claim issues a credential that verifies, carries no tier and no national identifier. **Issued by CREST's own signer, not Certify** — see the row below | E2E | partial |
| Credential signature and tampering | [#16](../../issues/16) | `pkg/credential` — a signed credential verifies; reassigning the subject, re-pointing the proof at another key, or verifying under a stranger's key all fail | Unit | covered |
| Issuance through Inji Certify | [#16](../../issues/16) | **Not proven for CREST's own credentials.** Certify does issue a `WorkEventCredential` over OpenID4VCI — see the P0 spike rows — but from a CSV fixture, not from CREST's evidence store, and nothing validates that fixture against the schemas. The CREST-side path is unbuilt | E2E | planned |
| Credential revocation | [#16](../../issues/16) | `make test-e2e` — revoking flips a bit in the signed Bitstring Status List and verification refuses the credential; the list is fetched whole so the check reveals nothing | E2E | covered |
| Identity provider interface | [#17](../../issues/17) | Same contract satisfied by ≥2 provider classes (eSignet + mobile OTP) | Contract | planned |
| Six OpenAPI descriptions | [#17](../../issues/17) | `schemas/openapi/*.yaml`, one per service, referencing `schemas/` by `$id` rather than restating it. Hand-written from the handlers — nothing yet checks they have not drifted | Contract | partial |
| Identity assurance is derived, never stored | [#20](../../issues/20) | `registry` computes it from a Party's bindings on request; there is no column to store it in (§4.1) | E2E | covered |
| Bednet definition, three faces | [#18](../../issues/18) | Seeded through the real endpoints, ratified and activated by the harness, and its tier map drives both the strength vectors and the live verdict. Resolving it from DeDi is still to come | E2E | partial |
| A definition's author is never its ratifier | [#21](../../issues/21) | Enforced in the definitions service and in a CHECK constraint; the harness seeds through DRAFT → ratify → activate, so the path is exercised on every run (§7) | E2E | covered |

## The spine (G2, #25)

Proven by `make test-e2e`: docker compose up, seed through the real endpoints, drive the clock, tear down. Three consecutive clean runs required (#40).

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| CSV in, verifiable credential out | [#25](../../issues/25) | `TestARecordBecomesACredentialAndAPayment` — a batch becomes a unit and a claim, the worker is notified, confirming issues a credential that verifies, and a payment instruction is released for the right amount | E2E | covered |
| The E2E harness itself | [#40](../../issues/40) | `make test-e2e` from a clean checkout with no manual step; readiness by polling, never `sleep`; the clock is driven rather than waited on | E2E | covered |
| W2 — the worker is told before it counts | [#23](../../issues/23) | The window and its notification commit together; the SMS arrives and says the worker will be paid either way | E2E | partial |
| A worker who was never reached is not auto-confirmed against | [#23](../../issues/23) | `TestAnUnreachedWorkerIsNotAutoConfirmedAgainst` — the notification outcome is recorded on the window, the sweep skips `unreached` windows and reports them, and the supervisor-assisted route closes them. Found by review: notify answers 201 for a failed send, so "notified" never meant "told" | E2E | covered |
| A re-submitted batch does not pay twice | [#22](../../issues/22) | `TestResubmittingTheSameBatchDoesNotPayTwice` — a unit's identity is derived from the work it describes, so two ingestions converge. Found by review: the unit id was minted fresh each time and the schema's own comment claimed otherwise | E2E | covered |
| A zero-outcome record is held, not paid as zero | [#26](../../issues/26) | `TestAZeroOutcomeIsHeldWithAReasonRatherThanPaidAsZero` — held with an explanation and an owner rather than RELEASED for nothing | E2E | covered |
| No raw national identifier reaches the unclear queue | [#22](../../issues/22) | `TestAnUnmatchedNationalIdentifierIsNeverStoredRaw` — the queue keeps the salted hash so a row can still be re-attributed. Found by review: the fixtures all joined on a phone, so this path was never exercised | E2E | covered |
| Canonicalisation matches RFC 8785 | [#16](../../issues/16) | `pkg/credential` — `&`, `<` and `>` are not escaped. Found by review: Go's `json.Marshal` escapes them, both sides of CREST agreed, and no conformant outside verifier would have | Unit | covered |
| W3 — silence is not consent | [#23](../../issues/23) | `TestSilenceStillPaysAndStaysDisputable` — seven days advance, the sweep auto-confirms, payment is released, and the claim is still disputable afterwards | E2E | covered |
| Every T=7 exit releases payment | [#23](../../issues/23), [#26](../../issues/26) | `TestADisputeStillReleasesPayment` for the hardest exit, plus `/v1/unreleased` asserted empty. All four routes are now exercised — self, auto, dispute, and supervisor-assisted via the unreached path | E2E | covered |
| W5 — a disputed claim never destroys the unit | [#22](../../issues/22) | The unit is fetched after its claim is disputed and is unchanged; `Contest.target` cannot name a unit at all | E2E | covered |
| W1 — an unattributable row goes to the unclear queue | [#22](../../issues/22) | `TestAnUnattributableRowGoesToTheUnclearQueue` — the row is queued with a reason and a row reference, never guessed at and never dropped | E2E | covered |
| W7 — probable matches hold | [#20](../../issues/20) | The registry answers 409 and writes a hold row. **The 409 path is not yet exercised end to end** — no fixture has two parties sharing an identifier | Contract | partial |
| A re-assessed source downgrades without reissuance | [#27](../../issues/27) | `TestReassessingASourceDowngradesAnUnchangedCredential` — the same credential bytes verify at a lower tier, and the verdict says why. Lifting the assessment restores it | E2E | covered |
| Withdrawal is visible to a verifier | [#27](../../issues/27) | `TestARevokedCredentialStopsVerifying` | E2E | covered |
| The tier is never stored | [#5](../../issues/5), [#27](../../issues/27) | The credential is asserted to carry no `tier` field; the verdict computes one per request | E2E | covered |
| W6 — verifiable without CREST | [#27](../../issues/27) | **Not proven.** `verification` resolves the issuer key, the status list and the definition over HTTP. The credential now carries `evidenceFields` so a tier map can be evaluated offline, but no offline verifier exists and the printed-card path (#1) is untouched | E2E | planned |
| W10 — a held payment has a reason and an owner | [#26](../../issues/26) | A CHECK constraint makes a hold without an owner unrepresentable, and `/v1/reconciliation` reports gaps that have no reason. **No scenario yet forces a hold** | E2E | partial |
| The outbox survives a crash between a state change and its side effect | [#47](../../issues/47) | **Not proven.** The transaction is structurally right; nothing yet kills a service mid-exit and asserts recovery | E2E | planned |

## Registry and definitions

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| Org self-registration + terms + approval | [#20](../../issues/20) | `TestAnOrganisationCannotApproveItself` and `TestAnOrganisationCannotBeApprovedBeforeAcceptingTerms` (E2E), plus a CHECK constraint that refuses a self-granted approval independently of the code | E2E | covered |
| Assisted enrolment — a worker with no phone | [#20](../../issues/20) | `TestAWorkerWithNoPhoneCanStillBeEnrolled`: the enrolment succeeds, the enroller is recorded and readable, and the worker's derived assurance is **not** raised by having been vouched for | E2E | covered |
| Public/private placement rule | [#20](../../issues/20) | `TestOrganisationFaceCarriesNoContactOrIdentityData` asserts on the serialised document, not the map keys, so a nested field cannot pass; `TestAWorkerNeverReachesTheRegistrySubstrate` (E2E) checks the publication surface holds no worker | Unit + E2E | covered |
| Only organisations' authorizations are published | [#20](../../issues/20), [finding #68](../../issues/68) | `TestAuthorizationFaceRefusesAWorkersAuthorization`. §3 says authorizations are public without qualifying which; a public log of workers' authorizations is a permanent roster of who works where | Unit | covered |
| Duplicate hold queue | [#20](../../issues/20) | **`merges_without_confirmation = 0`** — probable match holds, never auto-merges. The 409 path still has no fixture with two parties sharing an identifier (see the W7 row) | Unit + E2E | partial |
| DeDi abstraction / Postgres fallback | [#20](../../issues/20) | `pkg/dedi` behind one `Publisher` interface. The whole E2E suite runs on the fallback in CI; the node path is proven on demand against the deployed node (`make verify-registry`). Not "the same suite against both backends" — the two are deliberately *not* equivalent, and `Receipt.Transparent` is how a caller sees the difference | Contract + E2E | partial |
| DeDi's publisher signature, re-implemented | [#20](../../issues/20) | `TestPreimageWireContract` pins the exact preimage bytes against vectors generated from DeDi-node's own `internal/publisher`, so a drift in either implementation fails at `make test-unit` rather than as "signature does not verify" in a deployment | Unit | covered |
| A signed registry write uses wall time, not the domain clock | [#20](../../issues/20) | `TestSigningTimestampIsWallTimeNotTheDomainClock`. Found the hard way: services run on a driveable clock at the fixture epoch, and signing with it made every write to the deployed node fail on replay protection | Unit | covered |
| Author ≠ approver on definitions | [#21](../../issues/21) | Self-approval rejected in code and in a CHECK constraint; the harness seeds through DRAFT → ratify → activate on every run | Unit + E2E | covered |
| Definition immutable once ACTIVE | [#21](../../issues/21) | Edit rejected; a new version is the only way to change one; old versions still resolve | Unit + E2E | covered |
| An ACTIVE definition is resolvable outside CREST | [#21](../../issues/21) | Publication is enqueued in the same transaction as the ACTIVE transition, so a crash cannot leave a definition credentials pin and no verifier can resolve. `TestAnActivatedDefinitionIsResolvableOutsideCREST` (E2E) checks the publication exists; the deployed node was checked by hand with `make verify-registry`, and an inclusion proof validated by `tools/spikes/dediproof` — a second implementation | E2E + spike | partial |
| A pinned registry lookup returns the version it pinned | [#20](../../issues/20) | `TestResolveRefusesWhenThePinIsIgnored`. DeDi ignores an unrecognised query parameter and answers with the latest version and a valid proof (spike finding 3), so `pkg/dedi` compares what came back with what was asked for and refuses | Unit | covered |

## Evidence and confirmation

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| CSV adapter | [#22](../../issues/22) | Golden CSVs: clean, malformed, duplicate, unmatched-identity | Contract | planned |
| Validation against ACTIVE definition | [#22](../../issues/22) | Record violating the definition rejected with a usable reason | Unit | planned |
| Identity matching via hashed keys | [#22](../../issues/22) | Match provenance records *which key matched*; ambiguous match → unclear queue | Unit | planned |
| Unclear queue has an owner | [#22](../../issues/22) | No record can be dropped silently; queue depth is observable | E2E | planned |
| T=7 state machine | [#23](../../issues/23) | All transitions incl. disallowed ones; clock driven by harness, never slept | Unit | planned |
| Assisted enrolment + voice consent | [#24](../../issues/24) | Consent artefact retrievable and bound to the enrolment; worker with no phone completes | E2E | planned |

## Payments and verification

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| PaymentInstruction idempotency | [#26](../../issues/26) | Duplicate emission pays once — asserted at the rail sandbox, not just in-process | Unit + E2E | planned |
| Reconciliation with owned reasons | [#26](../../issues/26) | Injected gaps of each class; every one lands with an owner | E2E | planned |
| Advisory mode (no rail) | [#26](../../issues/26) | Full flow with rail disabled produces a statement, not an error | E2E | planned |
| Trust-chain walk | [#27](../../issues/27) | Broken chain at each link rejected: credential → definition → authorization → org → instance | Unit | planned |
| Every trust-chain link says whether a verifier can check it | [#68](../../issues/68) | `TestALinkAlwaysSaysWhereOrWhat` and `TestIssuerLinkInheritsTheDefinitionsCheckability` (unit); the spine asserts every link either names where to check it or names what is being trusted instead. A chain given as a list of sentences conflates facts a verifier can confirm with facts CREST is telling them | Unit + E2E | covered |
| The definition link is checkable exactly when the definition reached a log | [#68](../../issues/68), [#21](../../issues/21) | `TestTheTrustChainSaysWhichLinksAVerifierCanCheck` compares the link's `checkable` against the publication's `transparent`, and — when checkable — **fetches the URL it points at**. A `how` nobody can fetch is the promise this field replaces. Passes in both modes: node and fallback | E2E | covered |
| A valid verdict states what it does not establish | [#68](../../issues/68) | The same scenario asserts `notEstablished` names the subject's authorization. A green verdict reads as "and this person was authorised to do this work", which is exactly what a deployment cannot demonstrate to a stranger — a disclosed limit is not the same as one the verifier discovers by assuming wrongly | E2E | covered |
| Per-request disclosure consent | [#27](../../issues/27) | Refusal recorded as a value; verification without consent rejected | E2E | planned |
| Batch verification caps | [#27](../../issues/27) | Cap enforced per the G1 decision; overage rejected and logged | Unit | planned |

## Operations

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| Harness runs from clean checkout | — | CI runs `make test-e2e` on a fresh runner; no manual step | E2E | planned |
| Deployment verification | — | `make verify-deploy ENV=staging` green after every deploy | Deploy | planned |
| Second adapter via config only | [#30](../../issues/30) | Adapter added with **zero L1 code changes** — diff asserted in review | Contract | planned |
| Metric contracts | [#31](../../issues/31) | Same numbers from all three consoles for one fixture world | E2E | planned |

---

## Known gaps

Recorded rather than quietly carried:

- **Nothing in the product is `covered` yet** — only the foundations, the P0 spikes and the registry-publication rows above. This file is the specification of proof, written before the thing it proves, which is the intended order.
- **A `spike` row is not a regression test.** Nothing re-runs `make spike-dedi` automatically, so a DeDi upgrade could break an assumption here without anything going red. #20 has now put DeDi behind a CREST interface with tests of its own, which closes half of this: the wire contract and the pin check are `covered`. The half that remains is that **nothing in CI talks to a real node.** CI runs the Postgres fallback, because the node needs publisher keys minted from a checkout. The node path is `make verify-registry`, run by hand.
- **A published fact is checked for inclusion, never for consistency.** An inclusion proof says a record is in *a* log; a consistency proof says the log was never rewritten, which is the property an independent verifier actually depends on. The node exposes `/dedi/log/proof/consistency` and nothing in CREST reads it yet. Carried over from the P0 spike's "not covered by this spike" list, and still true.
- **One P0 claim cannot be settled locally at all**: offline verification (#1) needs a device with its radios off. Pairwise subject identity (#3) was listed here too and that was wrong twice over: self-hosted eSignet runs the same code a sandbox runs, so it answered the question; and "production access" was never a sandbox at all but a relying-party registration by a country's identity authority, which is [#53](../../issues/53) and waits on a pilot geography.
- **Offline verification (W6) needs real hardware.** A container asserting it has no network is weaker evidence than a phone in a field with no signal. The plan schedules a field simulation in week 12; W6's status stays `partial` until that happens, however green CI is.
- **Rail behaviour is only ever proven against a sandbox** until production. Sandboxes lie about failure modes — treat the first real payment run as a test, with someone watching.
