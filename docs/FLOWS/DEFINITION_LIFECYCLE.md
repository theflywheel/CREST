---
title: A definition is written, proven dry, and ratified
---

# A definition is written, proven dry, and ratified

**J4 · P-3.** The author writes what counts as a unit of work section by section, runs the draft against a real sample through the real adapter and strength function without writing anything, and submits. A different party ratifies, naming what is still pending. The tier is derived on the dry run, never typed.

```mermaid
sequenceDiagram
  autonumber
  participant Au as Author
  participant Con as Console
  participant Df as core · definitions
  participant Ev as core · evidence
  participant Ap as Approver
  participant D as DeDi
  Au->>Con: writes the sections (scope, unit, period, parties, evidence tiers, source, template, mapping)
  Con->>Df: PUT /v1/definition-drafts/{id}/sections/{section}
  Au->>Con: runs the sample
  Con->>Df: POST /v1/definition-drafts/{id}/dry-run
  Df->>Ev: parse through the registered adapter
  Df->>Df: strength.Evaluate per row, derived, with reasons
  Df-->>Con: rows with tier and why · committed: false
  Au->>Con: submit
  Con->>Df: POST /v1/definition-drafts/{id}/submit
  Ap->>Con: reviews, names the pending fields, signs
  Con->>Df: POST /v1/definitions/{id}/versions/{v}/ratify
  Df->>Df: refuse if the ratifier is the author
  Df->>D: publish the version
  Con->>Df: POST /v1/definitions/{id}/versions/{v}/activate
  Con->>Df: POST …/payment-handoff (pricing goes to the rate owner)
```

## What holds it

- The tier map names which provenance reaches which tier under reference numbering (1 strongest); the source screen derives the ceiling a provenance can reach and says it is not stored.
- The dry run writes no unit, no source, no queue entry; it shows the same rows twice under different provenance to prove the tier is derived.
- A draft is mutable only while open; submission is one transaction; ratification records the event log with both actors named.

## Recordings

- [The author writes and submits](../assets/clean-slate-watch/J4-p3-author-writes-and-submits-the-definition-8x.mp4) (14 s at 8×)
- [The approver ratifies with the gaps named](../assets/clean-slate-watch/J4-p3-approver-ratifies-with-the-gaps-named.mp4) (22 s)

Screens: p3_1–p3_28. Next: [PAYMENT_SETUP.md](PAYMENT_SETUP.md).
