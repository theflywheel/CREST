---
title: The credential substrate
---

# The credential substrate

Not a plug point today, but integrated the same way: through one small surface the rest of the layer does not see past.

## Two credentials, two suites

| Credential | Signed by | Suite | Verified by |
|---|---|---|---|
| CREST's work-event credential | The deployment's issuer, seed in Vault | DataIntegrityProof | `POST /v1/verify`, and offline from the signature |
| The wallet credential | Inji Certify, the deployment's plugin | Ed25519Signature2020 | Inji Verify, natively |

Two suites because Inji Verify implements no DataIntegrity suite. Both sign the same fact; the wallet credential carries the newest confirmed work event, because the OpenID4VCI flow has no record-selection step. Both are recorded in the [test manifest](../test-manifest.md) with what each cannot show.

## What stays derived

The tier is never in either credential. A verifier computes it from the provenance the credential carries and the project's current assessment of its source; a downgrade or a clearance changes what every credential from that source verifies at, with no reissuance.

## What an offline verifier cannot see

A dispute. A contest is resolved at verification time from the deployment that holds it, so a bare QR scan shows a valid credential with no sign it is contested. This is stated on the verify door rather than hidden.

The lifecycle mapping is [CREST on Inji](../crest-inji-architecture.html); the Certify plugin, the Mimoto patch and the guest download path are in `infra/certify/` and [../DEPLOYMENT.md](../DEPLOYMENT.md).
