# What Certify is configured to issue, and what is deliberately absent

`work_events.csv` is the hand-authored evidence #1 asks for: three confirmed
work events, one per identity-assurance level, so the same credential shape can
be seen carrying strong and weak provenance. It is the **only** data source in
this deployment — no CREST service is involved, which is the whole point of the
spike. #16 replaces this file with a plugin that reads confirmed claims.

Everything in it is invented. No real person, no real programme, and the
`individualId` values are mock-identity fixtures that exist nowhere else.

## What the credential does not carry

**No tier.** The columns are `sourceClass`, `captureMethod`, `adapterRef`,
`receivedAt` and `sourceExposure` — the facts. A verifier computes the tier from
them at the moment it asks. Putting a tier in the credential would freeze a
judgement at issuance time and make it un-upgradeable when the worker's identity
assurance later improves, and it is the one thing a signed, offline-verifiable
artefact makes permanently wrong.

**No national identifier and no biometric.** The farmer sample this
configuration is derived from carries a `face` column holding a base64 photo;
there is no equivalent here and there must not be. The `individualId` reaches
Certify from the authorization server and is used to look the row up; it is not
a column in the credential.

**No worker name.** Nothing about the credential requires one, and a credential
that needs a name to be useful is a credential that cannot be shown selectively.
The subject is the holder DID the wallet binds at issuance.

## The columns that are CREST vocabulary

`sourceClass`, `captureMethod` and `sourceExposure` take values from
`schemas/primitives/common.schema.json`. They are enums there, and nothing here
validates them — a typo in this file produces a credential whose provenance the
strength function cannot read. That gap is real and is why #16's plugin should
build the record from the schema rather than from a spreadsheet.
