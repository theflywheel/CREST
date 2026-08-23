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
| Credential issuance → wallet → printed card → offline verify | [#1](../../issues/1) | Recorded demo; offline leg needs a real device with radios off | E2E | planned |
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
| Canonical evidence record | [#14](../../issues/14) | The schema exists and compiles. The adapter conformance suite arrives with the CSV adapter | Contract | partial |
| Source heartbeat / silence detection | [#14](../../issues/14) | Harness stops a source; asserts a silence alert fires rather than nothing happening | E2E | planned |
| Strength function `f` | [#15](../../issues/15) | `pkg/strength/testdata/vectors.json` — nine cases run as a table, readable by a second implementation in any language | Unit | covered |
| Transport never affects strength | [#15](../../issues/15) | One set of facts through all four transports must give one answer (§8) | Unit | covered |
| A credential resolves against its pinned definition version | [#15](../../issues/15) | The same facts under v1 and a stricter v2 give different tiers, and v1 does not move | Unit | covered |
| A re-assessed source downgrades without reissuance | [#15](../../issues/15), [#6](../../issues/6) | A `SourceAssessment` cap changes the tier for an unchanged credential, and the reasoning says so | Unit | covered |
| WorkEventCredential issuance | [#16](../../issues/16) | Issued credential verifies in Inji Verify; status list entry resolves | E2E | planned |
| Credential revocation | [#16](../../issues/16) | Status list flip observed by an independent verifier | E2E | planned |
| Identity provider interface | [#17](../../issues/17) | Same contract satisfied by ≥2 provider classes (eSignet + mobile OTP) | Contract | planned |
| Bednet definition, three faces | [#18](../../issues/18) | Authored in `tests/fixtures/world.yaml`, validates against the Definition schema, and its tier map drives the strength vectors. Resolving it from DeDi is still to come | Contract | partial |
| A definition's author is never its ratifier | [#18](../../issues/18), [#21](../../issues/21) | Asserted on the fixture definition. Enforcement in the definitions service is still to come (§7) | Contract | partial |

## Registry and definitions

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| Org self-registration + approval | [#20](../../issues/20) | Unapproved org cannot issue; approval transition audited | E2E | planned |
| Public/private placement rule | [#20](../../issues/20) | Assert no PII on the DeDi node and no public facts held only privately | E2E | planned |
| Duplicate hold queue | [#20](../../issues/20) | **`merges_without_confirmation = 0`** — probable match holds, never auto-merges | Unit + E2E | planned |
| DeDi abstraction / Postgres fallback | [#20](../../issues/20) | Same suite passes against both backends | Contract | planned |
| Author ≠ approver on definitions | [#21](../../issues/21) | Self-approval rejected in code, not convention | Unit | planned |
| Definition immutable once ACTIVE | [#21](../../issues/21) | Edit rejected; new version created; old version still resolves | Unit + E2E | planned |

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

- **Nothing in the product is `covered` yet** — only the foundations and the P0 spikes are. This file is the specification of proof, written before the thing it proves, which is the intended order.
- **A `spike` row is not a regression test.** Nothing re-runs `make spike-dedi` automatically, so a DeDi upgrade could break an assumption here without anything going red. That is accepted deliberately for now and closes when #20 puts DeDi behind a CREST interface with contract tests of its own.
- **One P0 claim cannot be settled locally at all**: offline verification (#1) needs a device with its radios off. Pairwise subject identity (#3) was listed here too and that was wrong twice over: self-hosted eSignet runs the same code a sandbox runs, so it answered the question; and "production access" was never a sandbox at all but a relying-party registration by a country's identity authority, which is [#53](../../issues/53) and waits on a pilot geography.
- **Offline verification (W6) needs real hardware.** A container asserting it has no network is weaker evidence than a phone in a field with no signal. The plan schedules a field simulation in week 12; W6's status stays `partial` until that happens, however green CI is.
- **Rail behaviour is only ever proven against a sandbox** until production. Sandboxes lie about failure modes — treat the first real payment run as a test, with someone watching.
