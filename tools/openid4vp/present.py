#!/usr/bin/env python3
"""The wallet leg of OpenID4VP, as a script (#27; Blueprint §5, "CREST on Inji").

Inji Verify's UI, with VP_SUBMISSION_SUPPORTED=true, creates a VP request and
shows a QR carrying `openid4vp://authorize?client_id=…&request_uri=…`. A wallet
scans it, fetches the signed request object, and posts a presentation to the
request's response_uri. This script is that wallet: given the requestId (the
`state` the UI's status poll names), it fetches the request object, builds an
ldp_vp holding the CREST WorkEventCredential exactly as issued, and submits it
as direct_post. With the transactionId it then reads the verifier's result.

It exists because the deployed Inji Web (0.15.0) cannot do this against the
pinned Mimoto (0.19.2) — the wallet-side presentation surface arrives in
Mimoto 0.20.0 (#230). Until that upgrade, this is how the OpenID4VP wrapping
of Inji Verify is exercised on the fleet. Stdlib only, on purpose.

    make openid4vp-present REQUEST=req_… CRED=credential.json [TXN=txn_…]
"""
import base64, json, os, sys, time, uuid, urllib.parse, urllib.request

VERIFY = os.environ.get("VERIFY_URL") or "https://crest-verify-production.up.railway.app/v1/verify"
state = sys.argv[1]
cred = json.load(open(sys.argv[2]))

def get(url, headers=None):
    req = urllib.request.Request(url, headers=headers or {})
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.status, r.read()

# 1. The request_uri the QR points at: a signed request object (JWT).
st, body = get(f"{VERIFY}/vp-request/{state}")
jwt = body.decode()
hdr, payload, _sig = jwt.split(".")
pad = lambda s: s + "=" * (-len(s) % 4)
h = json.loads(base64.urlsafe_b64decode(pad(hdr)))
p = json.loads(base64.urlsafe_b64decode(pad(payload)))
print("request object: alg", h["alg"], "kid", h["kid"])
print("  client_id", p["client_id"])
print("  response_uri", p["response_uri"], "response_mode", p["response_mode"])
print("  presentation_definition id", p["presentation_definition"]["id"],
      "descriptors", [d["id"] for d in p["presentation_definition"]["input_descriptors"]])
assert p["state"] == state

# 2. The presentation: the wallet's answer, holding the credential as issued.
vp = {
    "@context": ["https://www.w3.org/2018/credentials/v1"],
    "type": ["VerifiablePresentation"],
    "verifiableCredential": [cred],
}
submission = {
    "id": str(uuid.uuid4()),
    "definition_id": p["presentation_definition"]["id"],
    "descriptor_map": [{
        "id": p["presentation_definition"]["input_descriptors"][0]["id"],
        "format": "ldp_vc",
        "path": "$.verifiableCredential[0]",
    }],
}
form = urllib.parse.urlencode({
    "vp_token": json.dumps(vp),
    "presentation_submission": json.dumps(submission),
    "state": state,
}).encode()
req = urllib.request.Request(p["response_uri"], data=form,
                             headers={"Content-Type": "application/x-www-form-urlencoded"})
try:
    with urllib.request.urlopen(req, timeout=60) as r:
        print("direct_post ->", r.status, r.read()[:200])
except urllib.error.HTTPError as e:
    print("direct_post ->", e.code, e.read()[:400]); sys.exit(1)

# 3. What the verifier concluded.
txn = sys.argv[3] if len(sys.argv) > 3 else None
if txn:
    for _ in range(10):
        st, body = get(f"{VERIFY}/vp-result/{txn}")
        d = json.loads(body)
        print("vp-result:", json.dumps({k: v for k, v in d.items() if k != "vcResults"}))
        for vc in d.get("vcResults", []):
            print("  vc status:", vc.get("verificationStatus"), "type:", json.loads(vc["vc"]).get("type") if isinstance(vc.get("vc"), str) else vc.get("vc", {}).get("type"))
        if d.get("vpResultStatus") not in (None, "PENDING"):
            break
        time.sleep(2)
