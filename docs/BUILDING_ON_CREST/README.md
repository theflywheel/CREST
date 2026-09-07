---
title: Building on CREST
---

# Building on CREST

How to put a new product on the infrastructure layer. The payments application for community health workers is the first product on the substrate; this section is what it needed, written down so the second one needs less.

| Document | What it covers |
|---|---|
| [THE_REFERENCE_MATERIAL.md](THE_REFERENCE_MATERIAL.md) | The three source documents in `docs/reference/`, what each decided, and how the blueprint was derived from them |
| [PRIMITIVES.md](PRIMITIVES.md) | The eleven primitives and what a product uses each for |
| [LAYERING.md](LAYERING.md) | Sorting a product's needs into infrastructure, configuration and product, and what to do when the layer is missing something |
| [API_SURFACE.md](API_SURFACE.md) | The five boundaries a product calls, the conventions every call follows, and the outbox topics it can subscribe to |
| [FIRST_DAY.md](FIRST_DAY.md) | A product's first day in order: from an empty instance to a credential and a payment |
| [RULES.md](RULES.md) | The rules a product inherits, and the two the payments application adds |
| [LOCAL_DEVELOPMENT.md](LOCAL_DEVELOPMENT.md) | Running the stack, the seeded world, and the gates a change passes |
| [WORKED_EXAMPLE.md](WORKED_EXAMPLE.md) | A second product sketched on the same substrate: training attendance with a stipend |

