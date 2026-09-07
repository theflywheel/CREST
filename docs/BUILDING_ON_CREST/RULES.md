---
title: Rules a product inherits
---

# Rules a product inherits

These bind whoever builds on the layer, not only the payments application. Each has a place in the code that enforces it ([../ARCHITECTURE/INVARIANTS.md](../ARCHITECTURE/INVARIANTS.md)); a product must not build a path around any of them.

| Rule | What it means for a product |
|---|---|
| **Trust strength is derived, never stored** | Compute the tier when asked, from provenance and the tier map; never cache one, never accept one from a payload |
| **A unit and a claim are separable** | Contest the claim; never destroy the unit; never key a product record on "who did it" alone |
| **Never persist a raw national ID or biometric** | Not in a table, a log, a fixture, a dedupe key or a screenshot; the pipeline hashes it before anything is stored |
| **Probable matches hold; they never auto-merge** | A 409 with a hold is an answer, not an error to retry around; a merge needs the person and the custodian |
| **Consent gates collection and disclosure** | Enrolment consent before evidence; per-share consent before an institution collects more than the bare credential proves |
| **Every private read is scoped** | Name the project; be refused if you may not read it; never render a refusal as an empty success |
| **What is not built is named, not faked** | A screen with no backend says so on its face; a stand-in refuses to run where it could be mistaken for the real thing |

## The two the payments application adds

They bind any product that moves money on these records:

- **Every confirmation-window exit releases payment.** Confirm, dispute, auto-confirm, assisted: all four. A dispute contests the record; it never withholds the money.
- **Every held payment has a reason with an owner.** A worker must never see a missing payment with no explanation and nobody's name attached.

## Writing your own

Write your product's rules as sentences before its code, each naming the worker guarantee it serves (Blueprint §11). Put them in the product's own document the way the readout summary did for payments, and name in every pull request which one a change could break and how that was proven. A rule nobody can point to in a test is an aspiration.
