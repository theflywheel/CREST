---
name: sync-design-docs
description: Keep the CREST blueprint and plan true after the code or a decision changes them. Use when work contradicts the design, when a G1 decision is settled, or when a hook flags that a design section may be stale.
---

# Keeping the design of record true

`docs/crest-infrastructure-blueprint.html` is the **design of record**. That status is only worth something if it stays true — a blueprint that still poses a settled question, or describes a contract the code abandoned, is worse than no blueprint, because people trust it.

## The three triggers

**1. A decision was settled.** When G1 ratifies one of the six (#5–#10), §14 stops listing options for it and records the decision, its rationale and who owns the consequence. Close the decision issue with the same text. §14 becomes a decision log, not a backlog.

**2. Work contradicted the design.** The blueprint said the primitives are generic; expressing the payments profile needed a payments-specific field. The blueprint said adapter classes connect through configuration; the second one needed an L1 change. **These are findings, not inconveniences.** Open an issue with the `Design finding` template, correct the affected section, and say so in your summary.

**3. A contract changed.** New field on the canonical evidence record, changed credential shape, new API surface. The blueprint sections §5, §8, §13 must match what the code actually does.

## How to edit

The docs are self-contained HTML with a token-based design system. Match the surrounding markup — the same `<section id="sN">` structure, the same classes for tables and callouts, the same theme-aware SVG classes (`.db`, `.dc`, `.di`, `.dd`, `.da`, `.dg`) for diagrams. Never introduce a colour outside the defined tokens; the pages render in both light and dark and a hardcoded colour breaks one of them.

Section IDs `s1`…`s16` are referenced from every issue's Design reference footer. **Do not renumber them.** Add new material inside an existing section, or append a new one.

## The section map

§1 scope+HLD · §2 primitives (§2.1 profiles) · §3 registry on DeDi · §4 identity (§4.1 providers) · §5 credential substrate · §6 strength derivation · §7 definition lifecycle · §8 evidence ingestion · §9 consent · §10 payments boundary · §11 worker invariants W1–W10 · §12 extensibility · §13 API surface · §14 open decisions · §15 journey scope map · §16 gap register

## After editing

- Update `docs/test-manifest.md` if the change alters how something is proven.
- If an invariant changed, that is a serious change — W1–W10 are promises to workers, and weakening one needs a stated reason, not a quiet edit.
- Commit with the issue number, so the design's history and the work's history stay connected.

## What not to do

Do not fix a contradiction by editing the code to match a blueprint that is wrong, or by editing the blueprint to match code that is wrong, without deciding which one is actually right. The contradiction is information. Resolve it deliberately and record which way you went.
