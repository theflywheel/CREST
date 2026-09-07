---
title: The credential, the wallet, and verification
---

# The credential, the wallet, and verification

**J9 · V-1, V-2, W-1.** An accepted claim is issued as a credential signed by the deployment's issuer; the worker holds it in an encrypted device wallet and can carry it as a wallet-format credential through Inji. Anyone can verify it, online against the status list and the trust chain, or offline from the signature alone. The tier a verifier sees is derived at that moment from the credential's provenance and the project's current assessment of its source.

```mermaid
sequenceDiagram
  autonumber
  participant Vf as core · verification
  participant Vault as Vault
  participant Wd as Worker door
  participant Inji as Inji Certify / Web
  participant Ver as Verifier
  Vf->>Vault: sign with the issuer seed
  Vf->>Vf: store the credential; extend the status list
  Wd->>Vf: GET /v1/parties/{id}/credentials (private: the worker's own)
  Wd->>Wd: encrypt into the device wallet; export and restore work offline
  Wd->>Inji: OpenID4VCI issuance of the newest confirmed event
  Inji-->>Wd: wallet credential, a PDF with the QR
  Ver->>Vf: POST /v1/verify {credential, requestedBy?, purpose?}
  Vf->>Vf: signature · issuer key · status list · definition link
  Vf->>Vf: tier = strength(provenance), capped by the project's source assessment
  Vf-->>Ver: verdict with a checkable trust chain
  Vf->>Vf: record the presentation on the worker's trail
```

## What holds it

- Issuance is idempotent by claim and revalidates the claim before building; a retry after a crash returns the credential already issued.
- The credential carries the canonical evidence field names the definition required, so an offline verifier reaches the same tier.
- A source assessment is the project's: downgrading a source in one project caps nothing in another; lifting it restores every credential it produced, with no reissuance.
- Two suites, two verifiers: CREST's credential signs a DataIntegrity proof; the Inji wallet credential signs Ed25519Signature2020 because Inji Verify implements no other. An offline verifier cannot see a dispute; that is stated, not hidden.

## Recordings

- The verifier panels (recording `J8-j8-verifier-panels.mp4`) (24 s)
- The wallet: encrypted restore, offline verification and the Inji guest download (recording `crest-development-validation.mp4`) (2 m 17 s)

Screens: v1_1–v1_3, v2_1–v2_3, w1_15–w1_17. Next: [CONSENT_PER_SHARE.md](CONSENT_PER_SHARE.md).
