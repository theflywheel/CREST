---
name: work-a-ticket
description: Pick up a CREST GitHub issue and work it correctly — read its design reference, respect its dependencies, satisfy its "done when", and update the test manifest. Use when starting work on an issue number, or when asked what to work on next.
---

# Working a CREST ticket

## 1. Read before writing

```sh
gh issue view <N> -R theflywheel/CREST
```

Every issue carries a **Design reference** footer. Open those blueprint sections and read them. They are not decoration — the issue body is a summary, and the blueprint is the design of record. Where they disagree, the blueprint wins and the disagreement is a finding worth raising.

Note two things in particular:

- **"Done when"** — the acceptance criteria. This is what you are building toward, not "the code compiles".
- **Blocked by** — GitHub records real dependencies. If the issue shows unmet blockers, **stop and say so** rather than working around them. The blockers exist because the work would be written wrong without them; #16 (credential schema) before G1 is the clearest case.

## 2. Check the layer

If the issue is labelled `infra-l1`, apply the layering test before adding anything:

> If two deployments could reasonably disagree about it and both still be CREST, it does not belong in the infrastructure layer.

Country-specific rules, programme policy, rate tables, org onboarding thresholds — configuration. The primitives, the evidence contract, the credential shape — infrastructure.

## 3. Build, with the invariants in view

W1–W10 (Blueprint §11) are promises to the worker. When your change touches evidence, confirmation, payments or verification, name which invariant it could break and how you prevented it. The usual suspects:

- Any change to the T=7 machine → **all four exits must still release payment** (W4).
- Any change to matching → **`merges_without_confirmation = 0`** (W1, and it's a monitored metric, not a hope).
- Any change to identity → **no raw national ID or biometric persisted, anywhere** (W9).
- Any change to reconciliation → **every gap keeps an owner and a reason** (W10).

## 4. Test it

Load the `write-tests` skill. Non-negotiable: the feature gets a row in `docs/test-manifest.md` in the same change. A feature with no manifest row is unproven regardless of what CI says.

## 5. Findings get raised, not absorbed

Several issues name a specific way reality might contradict the design — a primitive needing a payments-specific field, an adapter class needing an L1 change, Inji not composing as documented. **These outcomes are results.** When one happens:

1. Open an issue with the `Design finding` template.
2. Correct the blueprint (load `sync-design-docs`).
3. Say so plainly in your summary.

Quietly patching around a design error is the one failure mode this project cannot afford, because the error survives into the pilot wearing a passing test.

## 6. Closing

Close only when the "done when" is genuinely satisfied. If the criterion turned out to be wrong, **edit the issue to say what the right criterion is and why**, then satisfy that. Do not close against a criterion nobody met.

Before closing, check:
- [ ] "Done when" satisfied, item by item
- [ ] Manifest rows added or moved to `covered`
- [ ] Blueprint updated if the work changed the design
- [ ] Board status moved (Blocked only ever means a *recorded* dependency is unmet)
