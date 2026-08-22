# CREST design documents

Everything the implementation builds against. Each is a self-contained HTML page — clone and open in a browser.

| Document | Role | Sections |
|---|---|---|
| [crest-infrastructure-blueprint.html](crest-infrastructure-blueprint.html) | **The design of record.** Where any question about *what CREST is* gets answered. | 16 (`#s1`…`#s16`) |
| [crest-implementation-plan.html](crest-implementation-plan.html) | How the blueprint becomes a running pilot: phases, gates, risks. | 5 (`#s0`…`#s4`) |
| [crest-inji-architecture.html](crest-inji-architecture.html) | How the Inji stack maps onto CREST's credential lifecycle. | 6 (`#s0`…`#s5`) |
| [TRACKING.md](TRACKING.md) | How the work is tracked in GitHub — epics, gates, dependencies. | — |
| [COMPONENTS.md](COMPONENTS.md) | What we run vs what we build, and the repository shape. | — |
| [TESTING.md](TESTING.md) | The four test layers, the harness contract, and why in-toto waits. | — |
| [test-manifest.md](test-manifest.md) | Every feature and how it is proven. Maintained by hand. | — |

## Blueprint sections

Issues cite these as "Blueprint §N". Anchors (`…blueprint.html#s5`) work when the file is opened locally; GitHub's web view shows HTML as source, so open the file rather than following an anchor on github.com.

| § | Section |
|---|---|
| §1 | Scope of the infrastructure layer + HLD |
| §2 | The eleven primitives (+ §2.1 profiles) |
| §3 | Registry architecture on DeDi |
| §4 | Identity (+ §4.1 identity-provider integration) |
| §5 | Credential substrate |
| §6 | Strength derivation |
| §7 | Definition lifecycle |
| §8 | Evidence ingestion contract |
| §9 | Consent and withdrawal |
| §10 | Payments boundary |
| §11 | Worker invariants W1–W10 |
| §12 | Extensibility |
| §13 | API surface |
| §14 | Open decisions + definition of done |
| §15 | Twelve-journey scope map |
| §16 | Cross-journey gap register |

## Source material

The three documents the design was derived from live in [`../reference/`](../reference/): the concept note (*A DPI for Livelihoods* v1.6), the Trusted Payments readout summary, and the Actor Journeys screen flows.

## Keeping these current

The blueprint is the design of record, which means it has to stay true. Two rules:

- **Decisions get minuted back into it.** When G1 settles the six open questions, §14 stops being a list of options and becomes a decision log. A blueprint that still poses a settled question is worse than no blueprint.
- **Design findings edit it.** Several issues name a specific way reality might contradict the design. When that happens, the correction lands here — not only in the issue thread where it was found.
