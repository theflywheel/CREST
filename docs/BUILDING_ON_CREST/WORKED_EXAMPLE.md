---
title: A worked example
---

# A worked example: training attendance with a stipend

A second product on the same substrate, sketched to show how little of it is new.

## The activity

A skilling programme runs cohorts. A trainee who attends a session has done a unit of the activity; a trainee who completes the course holds a credential a prospective employer can verify; a stipend is paid per session attended.

## Sorting it

| Need | Pile | How |
|---|---|---|
| Trainees, trainers, the training provider | L1 | Parties; "trainee" and "trainer" are roles on grants |
| The cohort | L1 | A Context, owned by the provider's coordinator |
| "One session attended" | L1 | A Definition: activity `session-attended`, unit `sessions`, period the session day, tier map by provenance |
| The attendance register (a tablet app, a signed sheet) | L1 + adapter | A Source per register; the tablet's export needs an adapter, the sheet is `csv-batch@1` with a mapping |
| Consent to be recorded | L1 | Enrolment consent per cohort |
| The trainee's say on each session | Product | A confirmation window, if the programme wants one; a rule of the product, not the layer |
| The stipend | Product | An application beside payments, or the payments application itself with a rate per session |
| "Completed the course" | Product | A derived fact over accepted claims, presented as its own credential or as the trail of session credentials |
| Which sessions count, how many for completion | L2 | The definition's faces and the provider's configuration |

## What is new

- A profile: the terms name `deliver-training`, `attest-attendance`; the definition's worker face says what a session is in a trainee's words.
- One adapter, if the tablet's export is not a delimited file.
- One application service, if the stipend rules differ from the payments application's; otherwise a rate on the definition and a mechanism under the provider.
- A door for the trainer, or a face in the console.

## What is not new

Parties, consent, identity binding, holds, the credential, verification, the registries, the harness, the design system, the fidelity gate. The trainee's credential verifies at an employer's with no account, offline, exactly as the health worker's does, because the layer did not anticipate either.
