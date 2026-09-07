---
title: Layering a product
---

# Layering a product

Before writing a line, sort each thing the product needs into one of three piles.

| Pile | Test | From the payments application |
|---|---|---|
| **Infrastructure (L1)** | Two deployments could not disagree and both be CREST | The evidence contract; the pairwise identity binding; the credential shape; consent gating ingestion; holds that never merge |
| **Configuration (L2)** | A deployment decides it; the code reads it | The seven-day window's length; rate tables; terms sets and their functions; the "paid either way" sentence; adapter column mappings; the consent floor |
| **Product (L3)** | Your application and its faces | The confirmation window itself; holds with owners; the activation gate; the four doors |

## Three questions that sort most things

1. **Could another product on the same records not want this?** Then it is not infrastructure. The confirmation window is the payments application's because a training-attendance product might not need one.
2. **Would two countries set this differently?** Then it is configuration. The window's length, the wording, which provenance reaches which tier.
3. **Is it about a person's guarantees?** Then it is infrastructure even if only one product uses it today. Consent, the hold rule, never a raw identifier.

## When the layer is missing something

A product will meet a primitive that needs a field, an adapter class that needs an L1 change, or a substrate that does not compose as documented. That is a design finding, and findings are results, not obstacles:

1. Open an issue with the **Design finding** template, naming the section of the blueprint it contradicts.
2. Correct the blueprint in the same change, and say so plainly.
3. Only then build the thing.

Quietly patching around a design error in the product is the one failure this project cannot afford, because the error then survives into a pilot wearing a passing test. The findings so far ([#63](https://github.com/theflywheel/CREST/issues/63), [#64](https://github.com/theflywheel/CREST/issues/64), [#117](https://github.com/theflywheel/CREST/issues/117), [#172](https://github.com/theflywheel/CREST/issues/172), [#180](https://github.com/theflywheel/CREST/issues/180)) are the pattern to follow.

## Where a product's code goes

| Kind | Place | Example |
|---|---|---|
| A service | `services/<product>` beside `services/payments`, its own schema and deployable | The payments application |
| A door | `frontend/apps/<door>`, sharing `frontend/packages/ui` and `frontend/packages/api` | The four doors |
| A profile | The fixture world and the terms a deployment publishes | `tests/fixtures/world.yaml` |
| An adapter | `adapters/<name>`, registered in `adapters/builtin` | `adapters/csv` |

A product never writes to a member's tables. It calls the five boundaries and subscribes to the outbox topics they emit ([API_SURFACE.md](API_SURFACE.md)).
