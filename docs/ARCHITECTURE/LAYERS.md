---
title: Layers
---

# Layers

| Layer | What it is | Where it lives |
|---|---|---|
| **L1 · Infrastructure** | The eleven primitives, the evidence contract, strength derivation, the credential shape, consent, the public registries. Identical in every deployment. | `services/core`, `pkg/`, `schemas/`, `adapters/` |
| **L2 · Configuration** | What a deployment decides and the code reads: terms sets, definitions, rate tables, the consent floor, message wording, provider choice, adapter mappings. | The registry's records, the deployment's environment, the console's setup screens |
| **L3 · Products** | Applications on the substrate and their faces. The payments application is the first; the four doors are its faces. | `services/payments`, `frontend/apps/*`, `apps/web` |

## The test

*If two deployments could reasonably disagree about it and both still be CREST, it does not belong in the infrastructure layer.*

Country rules, programme policy, rate tables and onboarding thresholds are configuration. Primitives, the evidence contract and the credential shape are infrastructure. The confirmation window's length — seven days in the community health programme — is programme policy, and the window itself belongs to the payments application, not the substrate.

## What the test decided, in practice

| Question | Answer | Why |
|---|---|---|
| Is a "Worker" a primitive? | No: a role a Party holds | Aadhaar did not anticipate Jan Dhan; the core must not anticipate payroll |
| Is the confirmation window infrastructure? | No: the payments application's | Another product on the same records may not need one |
| Is the wording of a notification infrastructure? | No, but two sentences are enforced at start-up | The "paid either way" sentence and the reply keywords must survive translation |
| Which adapter parses a source? | Configuration: the source's registration | A payload must never choose its own parser |
| Which tier a credential reaches? | Derived at query time from provenance and the tier map | A stored tier freezes a judgement verifiers should make afresh |

When reality contradicts this sorting — a primitive that needs a use-case field, an adapter class that needs an L1 change — that is a design finding: open an issue with the finding template and correct the blueprint. Quietly patching around it is the one failure this project cannot afford.

