---
title: Services and their responsibilities
---

# Services and their responsibilities

## The two deployables

| Deployable | Members | Responsibility | Must never |
|---|---|---|---|
| **`crest-core`** (`services/core`, local port 59000) | parties · definitions · evidence · verification · attestation | The infrastructure layer as one process. Each member keeps its own Postgres schema, migrations, outbox and route family; the process boundary is one, the API boundaries are five. | Persist a raw national identifier or biometric. Store a trust tier. Merge a probable identity match without a person's confirmation. |
| **`crest-payments`** (`services/payments`, local port 59006) | one | The payments application: rate ownership and versioned rates, payment mechanisms with their activation gates, instructions priced by the rate in force when the work happened, holds with an owned reason, reconciliation, the rail provider. | Withhold money on a dispute. Reprice a held instruction outside the activation gate. Leave a held payment without a named owner. |

## The five members of core

### parties

- **Owns:** Party, Terms, Authorization, Context, Consent.
- **Answers:** who exists, under what terms, on which projects, with what consent.
- **Responsibilities:** organisation registration and admission; identity bindings held as a pairwise subject reference and a salted hash; enrolment by an agent or by the person; consent moments with their artefacts in the object store; grants at instance and context scope; projects, invitations, roles and ownership; identifier resolution with duplicate holds that never auto-merge; recovery of a lost login through nominated contacts; the instance's self-description; publication of the public faces to the registry, refusing any fact that holds personal data.
- **Routes:** `parties`, `authorizations`, `consents`, `enrolments`, `organisations`, `projects`, `invitations`, `terms`, `terms-requests`, `holds`, `recoveries`, `resolve`, `instance`, `auth`, `coverage`, `quality-worklist`.

### definitions

- **Owns:** Definition (versioned), LinkedRecord.
- **Answers:** what counts as a unit of work, and what is attached to it.
- **Responsibilities:** the authoring wizard's drafts, section by section; validation; a dry run of a real sample through the real adapter and strength function that writes nothing; submission; ratification by a party other than the author, with pending fields named; activation of one version; the payment handoff; linked records such as a payment setup, attached by reference; the adaptor catalogue; publication of the version to the registry.
- **Routes:** `definition-drafts`, `definitions`, `adaptors`.

### evidence

- **Owns:** Unit, Claim, Source.
- **Answers:** where evidence comes from and what it becomes.
- **Responsibilities:** source registration with approved provenance and a registered adapter; the adapter registry; batch ingestion with the submitter authorised in the project; per-row validation against the definition's contract; worker matching through parties, gated on enrolment consent to this programme; redaction before anything is stored; deduplication by the work described; units and claims; the unclear queue a custodian resolves by hand; source heartbeats; the batch receipt; the outbox that opens windows.
- **Routes:** `sources`, `batches`, `units`, `claims`, `unclear`, `adapters`, `registry-reuse`.

### verification

- **Owns:** Credential, Presentation.
- **Answers:** is this real, how strong is it, and who has been shown it.
- **Responsibilities:** issuance from an accepted claim, signed with the issuer seed in Vault; the status list and revocation; custody transfer to a wallet; verification with a checkable trust chain, online and offline; per-project source assessments that cap a tier without reissuance; share requests the worker decides per presentation; the presentation trail; the printed card.
- **Routes:** `credentials`, `verify`, `status-list`, `issuer`, `source-assessments`, `presentations`, `presentation-requests`.

### attestation

- **Owns:** the review window over a Claim.
- **Answers:** has the worker been asked, and what did they say.
- **Responsibilities:** open a window on every claim; the acknowledgement token; the four exits — confirm, dispute, auto-confirm by the clock, supervisor-assisted — every one of which releases the payment obligation; contests over a disputed claim; the sweep; the unreached and unreleased lists, scoped to a project.
- **Routes:** `windows`, `claims/{id}/confirm`, `claims/{id}/dispute`, `claims/{id}/assist`, `contests`, `unreached`, `unreleased`, `sweep`.

## The payments application

- **Owns:** rate ownership, rates as versioned terms, mechanisms, instructions, holds, reconciliation.
- **Responsibilities:** assign a rate owner per definition; publish rates effective from a date, never edited; stand up a mechanism naming its owner; the activation gate (test disbursement, reconciliation agreed, statement agreed, batching recorded, qualification verified) that refuses readably; turn every window exit into an instruction priced by the rate in force when the work happened; hold with a code, an explanation and an owner when money cannot move; submit through the configured provider; reconcile.
- **Routes:** `definitions/{id}/rate-owner`, `definitions/{id}/rates`, `mechanisms`, `instructions`, `reconciliation`, `statements`, `providers/simulator/settle` (development only).

## Names that still resolve

`crest-registry`, `crest-definitions`, `crest-evidence` and `crest-verification` alias onto core, and `crest-confirmation` onto payments. `crest-seed` is a one-shot job. `/internal/*` routes are service-to-service, authenticated by service identity, and refused at every door.

