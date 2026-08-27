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
| `bookkeeping` | Finishing a piece of work — issue, manifest, PR, board |

## The rules that don't bend

**Trust strength is derived, never stored.** *(infrastructure)* Credentials carry provenance facts (`sourceClass`, `captureMethod`, `adapterRef`); the tier is computed at query time. A stored tier freezes a judgement verifiers should be free to make differently, and it cannot be upgraded when identity assurance later improves.

**A unit and a claim are separable.** *(infrastructure)* A `Unit` exists independent of who performed it; a `Claim` links a `Party` to it. A disputed claim must never destroy the underlying record.

**Every confirmation-window exit releases payment.** *(payments application)* Confirm, dispute, auto-confirm, supervisor-assisted — all four. A dispute contests the record; it does not withhold the money. The window's length — seven days in the CHW programme, the "T=7" older text still says — is programme policy (L2), and the window itself lives in the payments application, not the substrate ([#127](https://github.com/theflywheel/CREST/issues/127)). The rule binds whoever runs that application; the infrastructure neither knows nor enforces a window.

**Never persist a raw national ID or biometric.** *(infrastructure)* A pairwise subject reference and a salted hash, nothing else. This applies to fixtures too.

**Probable matches hold; they never auto-merge.** *(infrastructure)* `merges_without_confirmation = 0` is a monitored metric, not an aspiration.

**Every held payment has a reason with an owner.** *(payments application)* A worker must never see a missing payment with no explanation attached.

When you touch evidence, confirmation, payments or verification, name which one your change could break.

**These six are not Blueprint §11's W1–W10, and this file used to say they were.** §11 enumerates ten *worker guarantees* — "exist without document, phone or literacy", "provable to a stranger in a minute, offline" — which are promises to a person. The six above are engineering rules that serve those promises. Six cannot be ten, and code comments across the services currently cite W-numbers that resolve to different statements in §11. Reconciling the two is [#57](https://github.com/theflywheel/CREST/issues/57); until it lands, cite the rule by its sentence rather than by a number.

## The layering test

> If two deployments could reasonably disagree about it and both still be CREST, it does not belong in the infrastructure layer.

Country rules, programme policy, rate tables, onboarding thresholds → configuration. Primitives, evidence contract, credential shape → infrastructure.

## Findings are results, not obstacles

Several issues name a specific way reality might contradict the design — a primitive needing a use-case field, an adapter class needing an L1 change, Inji not composing as documented. When that happens: open an issue with the `Design finding` template, correct the blueprint, and say so plainly.

**Quietly patching around a design error is the one failure mode this project cannot afford**, because the error then survives into a pilot wearing a passing test.

## Everything goes through a pull request

**No direct pushes to `main`, by anyone, agent or human.** Branch, open a PR, let CI run, merge.

This is about traceability, not ceremony. A PR is where the reasoning for a change is written down next to the change itself, where a reviewer — human or agent — has something reviewable, and where CI has somewhere to report before the fact rather than after. On a system whose records decide whether someone gets paid, "how did this get here?" needs an answer better than a commit on `main`.

- **One PR per issue** where the issue is small enough; name the issue in the body with `Closes #n`.
- **The PR body carries the reasoning**: what changed, which invariant (W1–W10) it could break, and how it was proven. If the change is a design finding, say so and link the issue.
- **Do not merge your own work without CI green.** A red PR is information; merging past it destroys the information.
- **Say what you did not do.** A PR that silently drops half its scope is worse than one that states the gap.

**This rule is currently a convention, not a gate.** Branch protection and rulesets both require a paid plan on a private repository, so GitHub will not refuse a direct push to `main` today. Treat the rule as binding anyway; it becomes enforceable the moment the org is on a paid plan or the repo goes public, and the first thing to do then is turn it on.

## Conventions

- Work through GitHub issues; every one carries a Design reference footer and a "done when".
- Close only against a satisfied "done when". If the criterion was wrong, edit it and say why.
- **Board status `Blocked` means a recorded dependency is unmet** — nothing else.
- Every feature gets a `docs/test-manifest.md` row in the same change.
- Commands live in the `Makefile`. If a skill tells you to run something, it is a make target.
- Git author email for this repo is `mittal.yash12@hotmail.com`.

## Bookkeeping is part of the work, not after it

The issue's "done when", the `docs/test-manifest.md` rows, the PR body, the board status — these are not paperwork that follows the change. They are the only record of whether the change was proven, and they get skipped exactly when they matter most, which is when the work ran late.

Skipped bookkeeping does not leave a gap; it leaves a lie. An issue closed against an unmet criterion reads as satisfied. A feature with no manifest row reads as covered by whatever CI happens to be green. A board that says Done reads as shipped. The next person believes all three and stops checking, and on a system whose records decide whether someone gets paid, that is how an untested path reaches a pilot.

So finish the work by loading the `bookkeeping` skill, in the same change and the same PR. The `unproven-work` pre-commit hook names what looks unproven, but it warns rather than blocks — it is a reminder, not a gate, and it cannot tell whether a status value is honest.
