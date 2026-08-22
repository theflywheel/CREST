# CREST — working notes for agents

Verifiable work history for informal workers. The first use case is trusted payments for community health workers, and the records this system holds decide whether someone gets paid. That framing is not decoration; it is why several rules below are absolute.

## Read first

- `docs/crest-infrastructure-blueprint.html` — **the design of record.** Where any question about what CREST *is* gets answered. §1–§16, referenced from every issue.
- `docs/TESTING.md` — how anything is proven.
- `docs/test-manifest.md` — what exists and how each thing is validated.
- `docs/TRACKING.md` — epics, gates, dependencies, board conventions.

## Skills

| Skill | Use it when |
|---|---|
| `work-a-ticket` | Starting on an issue |
| `write-tests` | Adding or changing tests |
| `run-harness` | Testing end to end, or debugging across services |
| `review-work` | Before a PR, or after finishing an issue |
| `sync-design-docs` | The design changed, or a decision was settled |
| `verify-deploy` | After a deploy |

## The rules that don't bend

**Trust strength is derived, never stored.** Credentials carry provenance facts (`sourceClass`, `captureMethod`, `adapterRef`); the tier is computed at query time. A stored tier freezes a judgement verifiers should be free to make differently, and it cannot be upgraded when identity assurance later improves.

**A unit and a claim are separable.** A `Unit` exists independent of who performed it; a `Claim` links a `Party` to it. A disputed claim must never destroy the underlying record.

**Every T=7 exit releases payment.** Confirm, dispute, auto-confirm, supervisor-assisted — all four. A dispute contests the record; it does not withhold the money.

**Never persist a raw national ID or biometric.** A pairwise subject reference and a salted hash, nothing else. This applies to fixtures too.

**Probable matches hold; they never auto-merge.** `merges_without_confirmation = 0` is a monitored metric, not an aspiration.

**Every held payment has a reason with an owner.** A worker must never see a missing payment with no explanation attached.

These are W1–W10 (Blueprint §11). When you touch evidence, confirmation, payments or verification, name which one your change could break.

## The layering test

> If two deployments could reasonably disagree about it and both still be CREST, it does not belong in the infrastructure layer.

Country rules, programme policy, rate tables, onboarding thresholds → configuration. Primitives, evidence contract, credential shape → infrastructure.

## Findings are results, not obstacles

Several issues name a specific way reality might contradict the design — a primitive needing a use-case field, an adapter class needing an L1 change, Inji not composing as documented. When that happens: open an issue with the `Design finding` template, correct the blueprint, and say so plainly.

**Quietly patching around a design error is the one failure mode this project cannot afford**, because the error then survives into a pilot wearing a passing test.

## Conventions

- Work through GitHub issues; every one carries a Design reference footer and a "done when".
- Close only against a satisfied "done when". If the criterion was wrong, edit it and say why.
- **Board status `Blocked` means a recorded dependency is unmet** — nothing else.
- Every feature gets a `docs/test-manifest.md` row in the same change.
- Commands live in the `Makefile`. If a skill tells you to run something, it is a make target.
- Git author email for this repo is `mittal.yash12@hotmail.com`.
