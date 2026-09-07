---
title: Invariants and where they are enforced
---

# Invariants and where they are enforced

Six engineering rules serve the blueprint's ten worker guarantees (§11). They are absolute because the records this system holds decide whether someone gets paid.

| Rule | Layer | Enforced in |
|---|---|---|
| **Trust strength is derived, never stored** | infrastructure | `pkg/strength`; verification computes the tier on every verify; a per-project source assessment caps it with no reissuance; the console's dry run derives it and says so on the screen |
| **A unit and a claim are separable** | infrastructure | evidence: a Unit is keyed by the work it describes, a Claim links a Party to it; a dispute opens a contest over the claim and never deletes the unit |
| **Every confirmation-window exit releases payment** | payments application | attestation: all four exits enqueue a release; payments turns it into an instruction, held with a reason if it cannot move; the dispute path is proven to release in `TestADisputeStillReleasesPayment` |
| **Never persist a raw national ID or biometric** | infrastructure | `pkg/pii`; parties bindings; evidence redacts before any row is stored, including the unclear queue and the dedupe key; the pre-commit hook refuses fixtures that carry one |
| **Probable matches hold, never auto-merge** | infrastructure | parties holds: a 409 with a hold record, resolved only by the worker's own confirmation followed by the custodian's decision; `merges_without_confirmation` is a monitored metric |
| **Every held payment has a reason with an owner** | payments application | payments: a hold carries a code, an explanation and an owner party; the worker's door shows all three |

## How a change is checked against them

When a change touches evidence, confirmation, payments or verification, its pull request names which rule it could break and how that was proven. The test manifest (`docs/test-manifest.md` in the repository) carries a row per feature; the harness scenarios in `harness/scenarios` drive each rule on real services in CI. ../TESTING.md (`docs/TESTING.md` in the repository) explains the layers.

## The ten worker guarantees

The blueprint's §11 enumerates ten promises to a person — exist without a document, phone or literacy; be provable to a stranger in a minute, offline; and so on. The six rules above are the engineering that keeps those promises. Reconciling the two numberings is tracked in #57; until it lands, cite a rule by its sentence rather than a number.
