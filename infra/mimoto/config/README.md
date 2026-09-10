# What the wallet is configured to talk to

`mimoto-issuers-config.json` names exactly one issuer: this deployment's
Certify. `client_id` is a placeholder replaced at start-up from
`MIMOTO_OIDC_CLIENT_ID`, because the client is registered against eSignet by
`tools/certify/mimoto-onboard.py` and its id is derived from the key that tool
mints — a key that must not be in this repository.

`mimoto-trusted-verifiers.json` names this deployment's Inji Verify. A verifier
absent from that list cannot ask this wallet for a presentation, which is the
point: it is the wallet's own list of who may ask, not the verifier's claim
about itself.

Since 0.20.0 (#230) the file takes 0.20's shape (`redirect_uris`, plural) and
names the verify *service*'s DID as the client id — that is what the signed
request object behind Inji Verify's QR carries (`client_id`
`did:web:crest-verify-production.up.railway.app:v1:verify`, `response_uri`
`…/v1/verify/vp-submission/direct-post`). The UI origin stays as a second
entry for the older unsigned form.
