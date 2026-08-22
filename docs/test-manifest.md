# Test manifest

Every feature, and how we know it works. Maintained **by hand** — see [TESTING.md](TESTING.md) for why.

**Status values:** `planned` (no code yet) · `partial` (some layers proven) · `covered` (every listed check passes in CI) · `unproven` (code exists, validation does not — treat as a bug).

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

## Infrastructure layer

| Feature | Issue | How it is proven | Layer | Status |
|---|---|---|---|---|
| Eleven primitive schemas | [#12](../../issues/12) | Valid + malformed document pairs per primitive; round-trip through each service | Unit | planned |
| Trusted Payments profile | [#13](../../issues/13) | Profile expressible with zero primitive changes — asserted by schema diff against #12 | Contract | planned |
| Canonical evidence record | [#14](../../issues/14) | Adapter conformance suite: every adapter emits records passing the same validator | Contract | planned |
| Source heartbeat / silence detection | [#14](../../issues/14) | Harness stops a source; asserts a silence alert fires rather than nothing happening | E2E | planned |
| Strength function `f` | [#15](../../issues/15) | Published test vectors: every tier × every assurance level, plus retroactive upgrade | Unit | planned |
| WorkEventCredential issuance | [#16](../../issues/16) | Issued credential verifies in Inji Verify; status list entry resolves | E2E | planned |
| Credential revocation | [#16](../../issues/16) | Status list flip observed by an independent verifier | E2E | planned |
| Identity provider interface | [#17](../../issues/17) | Same contract satisfied by ≥2 provider classes (eSignet + mobile OTP) | Contract | planned |
| Bednet definition, three faces | [#18](../../issues/18) | Definition resolves from DeDi; all three faces render; tier map applied | E2E | planned |

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

- **Nothing is `covered` yet** — the code does not exist. This file is the specification of proof, written before the thing it proves, which is the intended order.
- **Offline verification (W6) needs real hardware.** A container asserting it has no network is weaker evidence than a phone in a field with no signal. The plan schedules a field simulation in week 12; W6's status stays `partial` until that happens, however green CI is.
- **Rail behaviour is only ever proven against a sandbox** until production. Sandboxes lie about failure modes — treat the first real payment run as a test, with someone watching.
