---
title: The primitives
---

# The primitives

Eleven generic primitives that know nothing about work or money, and profiles that instantiate them per domain. The generic pattern: **a defined activity, an attested event, an owned claim, and trust derived from provenance.**

| Primitive | What it asserts | Your product uses it as |
|---|---|---|
| **Party** | An actor exists, person or organisation; identity binds late; carries an optional map of self-declared descriptive attributes | Whoever does, attests, verifies or pays. "Worker" is a role a Party holds, not a type |
| **Terms** | A published, versioned permission set a Party may qualify under | What an organisation is admitted on; the vocabulary of functions your product's roles are made of |
| **Authorization** | Party X may exercise functions F, in scope G, for period T, per authority A; evidenced, approved, never transferable; a context grant is always a subset of an instance one | Every role, at instance or context scope |
| **Context** | A bounded operational scope with its own configuration and activation gates, and an acknowledged owner | A project, a programme, a cohort |
| **Definition** | What counts as one unit of the activity: versioned, dry-run, ratified by a different party, activated | Your unit of work, attendance, or delivery, with its tier map |
| **Source** | A registered origin of evidence, with approved provenance and a registered adapter | Each system that reports the activity |
| **Unit** | One instance of the activity, measured under a definition version, independent of who performed it | The thing that happened |
| **Claim** | A Party performed a Unit; matched by key or by a person's decision | Who gets credit, or paid |
| **Consent** | A recorded consent moment, per programme, with its artefact, revocable | The person's say over collection and disclosure |
| **Credential** | An accepted Claim, signed, verifiable anywhere | What the person carries away |
| **Contest** | A challenge to a Claim, decided by a person | The dispute path that never withholds |

Two more objects ride on these: **LinkedRecord** (a payment setup, a rate; attached to a definition version by reference, never inlined) and **Presentation** (one disclosure of a credential, on the person's trail).

## Profiles

A profile is the domain's vocabulary over the primitives: which functions the terms name, what a definition's faces say to a worker, a verifier and a source system, which provenance classes reach which tier. The community health profile is the fixture world in `tests/fixtures/world.yaml`; a new product writes its own.

## What is deliberately not a primitive

Worker, ProjectGrant, CompensationRecord, Qualification. Each was in an early draft and each was the payments use case leaking into the layer; the blueprint's §2 records the collapse. If your product seems to need one, it is a profile or a product object, not a primitive.

The canonical table, with the genericity note for each row, is [§2 of the blueprint](../crest-infrastructure-blueprint.html#s2); the JSON Schemas in `schemas/` are the source of truth for every field.
