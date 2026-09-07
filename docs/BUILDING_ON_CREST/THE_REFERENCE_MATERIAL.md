---
title: The reference material
---

# The reference material

The design of record, the [Infrastructure Blueprint](../crest-infrastructure-blueprint.html), was derived from three documents. They live in [`docs/reference/`](https://github.com/theflywheel/CREST/tree/main/docs/reference) and are the requirements authority when the blueprint and the code disagree about intent.

| Document | Form | What it decided |
|---|---|---|
| **A DPI for Livelihoods** (v1.6) | PDF, the concept note | CREST is infrastructure, not an app: verifiable work history for informal workers, built so that the first use case does not leak into the layer. The warning the blueprint quotes — Aadhaar did not anticipate Jan Dhan — is this document's. |
| **CREST — Trusted Payments Use Case, Readout Summary** | DOCX | The first product: trusted payments for community health workers. Payment released on every exit of a confirmation window; a dispute contests the record, never the money; a held payment always has an owner. |
| **CREST — Actor Journeys** (17 August) | HTML, the screen flows | Twelve journeys and their personas (G-1 … V-4), every screen with its fields, callouts and buttons. `docs/journey-spec.json` is generated from it and the fidelity gate holds every built screen to it. |

## How the blueprint uses them

- The concept note gives the **layering test** and the primitive model: eleven generic primitives, profiles per domain.
- The readout gives the **payments application** its rules, and by ruling them application-level (Blueprint §10, [#127](https://github.com/theflywheel/CREST/issues/127)) shows where a product's rules stop and the layer's begin.
- The journeys give the **scope map** (§15): per journey, what L1 must provide, what is L2 data, what stays L3 product. A screen the backend cannot serve is drawn and says so, never faked.

## When you build a product

Read the concept note for the layer's intent, the readout for how a product's rules were written down, and the journeys for what a finished product's screens are held to. Then write your product's equivalent of the readout: its rules, in sentences, with the invariant each one serves.

