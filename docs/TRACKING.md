# How work is tracked

The [implementation plan](crest-implementation-plan.html) is the narrative; GitHub is the state. This file explains how the two line up so nobody has to guess where a piece of work lives.

## The hierarchy

```
Milestone (a phase, with a due date)
  └── Epic issue (why this phase exists, what it retires)
        └── Sub-issues (the actual work)
```

Six epics, thirty-three work issues. Every work issue is a **native sub-issue** of exactly one epic, so each epic shows a live completion count without anyone maintaining a checklist by hand.

| Epic | Milestone | Due | Retires |
|---|---|---|---|
| [#34](../../issues/34) P0 — Substrate spike | P0 | 2026-08-22 | "does the stack do what the docs say?" |
| [#35](../../issues/35) G1 — The six decisions | P1 | 2026-08-23 | undiscovered decisions |
| [#36](../../issues/36) P1 — Specification bundle | P1 | 2026-08-23 | design errors, at document cost |
| [#37](../../issues/37) P2 — Core loop slice | P2 | 2026-08-23 | integration risk |
| [#38](../../issues/38) P3 — Payments + verification | P3 | 2026-08-23 | money movement, trust resolution |
| [#39](../../issues/39) P4 — Inclusion, oversight, federation | P4 | 2026-08-23 | pilot-scale operational risk |

## Dependencies are recorded, not remembered

Blocking relationships use GitHub's native issue dependencies, so a blocked issue says so on its own page:

- **#4** (findings memo) ← the three P0 spikes (#1, #2, #3)
- **#12** (primitive schemas) ← #4 — schemas stay open until reality reports back
- **#15** (strength function) ← #8 — test vectors need the external-source tier decided
- **#16** (credential schema) ← #5, #11 — **the one true gate.** Tier semantics change the credential's field set; it cannot freeze before G1
- **#22** (evidence pipeline) ← #18 — validation needs a real definition to validate against
- **#25** (G2 demo) ← all of P2
- **#26** (payments runtime) ← #19 — rail sandbox lead time is external, which is why the conversation starts in P1
- **#30** (second adapter) ← #22
- **#33** (G3 readiness) ← all of P4

If you find yourself explaining a blocker in a comment, record it as a dependency instead.

## Labels

**Phase** — `phase-0` … `phase-4`. Where the work sits in the plan.

**Kind** — `epic` (tracking issue), `spike` (time-boxed, output is knowledge not code), `spec` (schema or contract), `gate` (a deliverable that unblocks a phase), `decision` (blocked on people, not code), `infra-l1` (touches the infrastructure layer, so the extensibility test applies).

`infra-l1` is the one that carries a rule: if a change makes two reasonable deployments unable to disagree, it belongs in L1; if they could disagree and both still be CREST, it belongs in configuration instead.

## The three gates

Gates are issues, not calendar entries, because they have acceptance criteria and can fail.

- **G1** (#11) — six decisions ratified and minuted into the blueprint. Blocks the credential schema freeze. **If inconclusive, the blueprint default ships** and the question is revisited with field data; a deferred decision is fine, an undiscovered one is not.
- **G2** (#25) — the slice demo. Real CSV in, credential in a wallet and on a printed card, verified offline by a stranger. No hand-authored data anywhere in the path.
- **G3** (#33) — pilot readiness. W1–W10 as executable acceptance tests, security and data-protection sign-off, one partner org onboarded without engineering help.

## Conventions

- **Close an issue only when its "done when" is satisfied** — every work issue states one. If the criterion turned out to be wrong, edit it and say why, rather than closing against a criterion nobody met.
- **Design findings get raised, not patched around.** Several issues name a specific failure the work might expose — a primitive that needs a payments field, an adapter class that needs an L1 change. Those outcomes are *results*, and the correct response is a new issue, not a quiet workaround.
- **Milestone due dates are agent-paced, not human-paced.** The whole board is scheduled to close between 2026-08-22 and 2026-08-23. The original plan's week numbers were sized for a 5–7 engineer human team and are wrong for how this repository is actually built; they have been removed rather than left to mislead.
- **Ordering still binds even when the calendar collapses.** A phase's dependencies must be satisfied before it closes — compressing the schedule does not license closing #12 before #4, and the recorded dependencies are what enforce that.
- **Gates can still fail.** G1, G2 and G3 have acceptance criteria; hitting them in hours rather than weeks does not lower the bar, and a gate that cannot be met honestly stays open.

## Project board

**[CREST Delivery](https://github.com/orgs/theflywheel/projects/2)** — all 39 issues, with three single-select fields:

- **Phase** — mirrors the milestones (P0 … P4).
- **Kind** — Epic / Spike / Spec / Build / Decision / Gate.
- **Status** — Todo / In Progress / **Blocked** / Done. `Blocked` means a *recorded* dependency is unmet, nothing else; if work is stuck for a different reason, that reason belongs in a comment or a new issue.

Nine items start Blocked (#4, #12, #15, #16, #22, #25, #26, #30, #33) — every one of them waiting on something real, not on capacity.

The board is a **view**, not a second source of truth. Milestones, epics and dependencies live on the issues; if the board and an issue disagree, the issue wins.
