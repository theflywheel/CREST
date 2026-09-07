---
title: CREST documentation
---

# CREST documentation

**Certified Record of Employment, Skilling and Training**: digital public infrastructure for verifiable work history for informal workers. The first product on it is trusted payments for community health workers, and the records this system holds decide whether someone gets paid.

The test every boundary here answers to: *if two deployments could reasonably disagree about it and both still be CREST, it is not infrastructure.*

| Section | What it holds |
|---|---|
| [Architecture](ARCHITECTURE/) | The high-level design, the layers, every component and its responsibilities, the substrate, the shared libraries, the six invariants and where each is enforced, the data boundaries |
| [Flows](FLOWS/) | Every flow the system carries in the order a deployment meets them, each with its sequence diagram |
| [Plugins](PLUGINS/) | The five things a deployment swaps — evidence adapters, payment providers, identity providers, the registry substrate, notification senders — the credential substrate, and how to add one |
| [Building on CREST](BUILDING_ON_CREST/) | How to put a new product on the infrastructure layer: the reference material, the primitives, the layering test, the API surface, a product's first day, the rules it inherits, a worked example |

Start with [the high-level design](ARCHITECTURE/HLD), then [a product's first day](BUILDING_ON_CREST/FIRST_DAY).

The design of record (the Infrastructure Blueprint), the implementation plan, the test manifest and the working notes live in the repository under `docs/`; these pages name them where they apply.
