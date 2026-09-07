---
title: Standing the instance up
---

# Standing the instance up

**J2 · G-1.** A deployment starts with nobody in it. `tools/bootstrap-operator` prints a one-time claim link; whoever opens it and signs in becomes the instance operator, and the instance's self-description is published to the registry. Nothing can be admitted until the operator has published a terms set.

```mermaid
sequenceDiagram
  autonumber
  participant Op as Operator
  participant Con as Console
  participant IdP as eSignet
  participant P as core · parties
  participant D as DeDi
  Op->>Con: opens the claim link
  Con->>IdP: login
  IdP-->>Con: token (pairwise subject)
  Con->>P: POST /v1/instance/setup
  P->>P: bind subject → the configured operator party
  P->>D: publish the instance self-description
  P-->>Con: instance, operator
  Op->>Con: publishes a terms set
  Con->>P: POST /v1/terms
  P->>D: publish the terms (a public face)
```

## What holds it

- The setup call is accepted only from the subject the deployment was configured with (`CREST_SETUP_SUBJECT_REF`, `CREST_SETUP_ISSUER`); a second setup is refused.
- The operator party id is deployment configuration (`CREST_OPERATOR_PARTY_ID`); the fixture world's is the organisation `…RGN`.
- Terms are versioned and public; a grant can never exceed the terms it cites (`wider_than_terms`).

## Recordings

- The operator claims and stands up (recording `J1-g1-operator-claims-and-stands-up.mp4`) (39 s)
- The operator publishes terms (recording `J1-g1-operator-publishes-terms.mp4`) (22 s)

Screens: g1_1–g1_6 in the journey traceability (`docs/journey-traceability.md` in the repository). Next: [ORGANISATION_ONBOARDING.md](ORGANISATION_ONBOARDING.md).
