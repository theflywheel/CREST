---
title: Architecture
---

# Architecture

The engineering view of the design of record, the [Infrastructure Blueprint](../crest-infrastructure-blueprint.html). Where the two disagree, the blueprint wins and this section is the defect.

The one test every boundary answers to: **if two deployments could reasonably disagree about it and both still be CREST, it is not infrastructure.**

| Document | What it covers |
|---|---|
| [HLD.md](HLD.md) | The high-level design: every component, what connects to what, and the path that ends in the worker's hands |
| [LAYERS.md](LAYERS.md) | Infrastructure, configuration and product: where each lives and how to tell them apart |
| [SERVICES.md](SERVICES.md) | The two deployables and the five members of core, with their responsibilities, primitives and routes |
| [DOORS.md](DOORS.md) | The four product doors, who uses each, and what each holds on the device |
| [SUBSTRATE.md](SUBSTRATE.md) | What is run rather than built: Postgres, Vault, DeDi, eSignet, Inji, and the local mocks |
| [LIBRARIES.md](LIBRARIES.md) | The shared Go packages and the responsibility each carries |
| [INVARIANTS.md](INVARIANTS.md) | The six rules that do not bend, and the code that enforces each |
| [DATA_BOUNDARIES.md](DATA_BOUNDARIES.md) | What goes to the public log, what stays in the private store, what lives on the device |

The roster with ports and deployed names is the older [../SERVICES.md](../SERVICES.md); the run-versus-build reasoning is [../COMPONENTS.md](../COMPONENTS.md). Both stay authoritative for what they cover.
