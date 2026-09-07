---
title: A product's first day
---

# A product's first day, in order

This is the order the clean-slate recordings in the [FLOWS section](../FLOWS/README.md) walk, and the order a new product must respect, because each step's record is the next step's precondition.

1. **Stand the instance up.** `tools/bootstrap-operator` prints the claim link; whoever opens it and signs in is the operator. The operator publishes at least one terms set. Nothing can be admitted before this. [INSTANCE_SETUP](../FLOWS/INSTANCE_SETUP.md)
2. **Admit an organisation.** It registers, accepts terms, declares its documents, submits its checks; the custodian decides. [ORGANISATION_ONBOARDING](../FLOWS/ORGANISATION_ONBOARDING.md)
3. **Create a context and grant roles.** A project with a configurator; invitations that become grants when claimed with the invitee's own login. Your product's roles are functions on a grant, named in the terms set: add the functions your product needs to the terms, which are configuration, rather than inventing a role store. [PROJECT_SETUP](../FLOWS/PROJECT_SETUP.md)
4. **Define the activity.** One definition per unit, dry-run against a real sample, ratified by someone other than its author, activated. The tier map is where you state which provenance reaches which tier; nothing stores the tier. [DEFINITION_LIFECYCLE](../FLOWS/DEFINITION_LIFECYCLE.md)
5. **Register sources.** One per system that will report, in the project, with the adapter it is parsed by and its provenance class. If no shipped adapter fits, write one and run it through the conformance suite. [EVIDENCE_ADAPTERS](../PLUGINS/EVIDENCE_ADAPTERS.md)
6. **Enrol people, with consent.** Either pathway. Consent is per programme and gates ingestion: a row about someone who has not consented to this context becomes an unclear row, not a claim. [ENROLMENT](../FLOWS/ENROLMENT.md)
7. **Ingest.** `POST /v1/batches`. Read the receipt; the unclear queue is a custodian's work, not a retry loop. [EVIDENCE_INGESTION](../FLOWS/EVIDENCE_INGESTION.md)
8. **Do what your product does with the claim.** For payments that is a window, an exit, an obligation, a rate, a mechanism. Build it as an application beside `services/payments`, consuming the substrate and subscribing to the outbox, never reading the members' tables. [CONFIRMATION_WINDOW](../FLOWS/CONFIRMATION_WINDOW.md), [PAYMENT_SETUP](../FLOWS/PAYMENT_SETUP.md)
9. **Let the credential do the rest.** Issuance happens on acceptance; verification needs nothing from your product; the person's consent per share is already theirs. [CREDENTIALS_AND_VERIFICATION](../FLOWS/CREDENTIALS_AND_VERIFICATION.md)

## What you will write, and what you will not

| You write | You do not write |
|---|---|
| A profile: the terms' functions, the definition faces, the tier map | A party store, a consent store, a role store |
| An application service with its own schema and its own rules in sentences | Anything that reads another member's tables |
| Doors, on the shared design system, deriving what a person sees from their grants | A role chooser |
| An adapter, if your sources need one | Parsing inside the application |
| Scenarios in the harness, a journey walk, manifest rows | A screen that fakes what has no backend |
