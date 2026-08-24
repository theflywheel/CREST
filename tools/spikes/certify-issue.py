#!/usr/bin/env python3
"""Issue a WorkEventCredential over OpenID4VCI, and verify it. (#1)

The point of this spike is that no CREST service is involved. A worker
authenticates at eSignet, a wallet-shaped client presents the resulting access
token to Certify, and Certify returns a signed credential built from a
hand-authored CSV row. If that works, the credential substrate is proven before
anything of ours depends on it; if it does not, we would rather know now than
after #16 has been built on top of it.

What it asserts, in order, because each step only means something if the one
before it did:

  1. Certify advertises a WorkEventCredential and names an authorization server.
  2. eSignet mints an access token for the credential scope, and the token's
     audience is Certify's credential endpoint.
  3. Certify returns a credential bound to a holder key the wallet proves it
     controls — not to whoever presented the token.
  4. The credential's issuer DID resolves, and its Ed25519 proof verifies
     against the key in that DID document.
  5. The credential carries provenance facts and NOT a tier — the one thing a
     signed offline artefact must not freeze.
  6. It carries no national identifier and no biometric.

Usage:
    python3 tools/spikes/certify-issue.py                  # deployed
    CERTIFY=http://localhost:58090 ESIGNET=http://localhost:58088 \
      MOCK_IDENTITY=http://localhost:58082 python3 tools/spikes/certify-issue.py

Requires: cryptography, pyjwt.
"""
import base64
import json
import os
import secrets
import sys
import time
import urllib.request

import jwt
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import ed25519

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from esignet_oidc import (  # noqa: E402
    ESIGNET,
    authorize,
    b64u,
    client_id_for,
    ensure_mock_identity,
    load_key,
    post,
    refresh_csrf,
    register_client,
)

CERTIFY = os.environ.get("CERTIFY", "https://crest-certify-production.up.railway.app")
INDIVIDUAL_ID = os.environ.get("INDIVIDUAL_ID", "9876543210")
PIN = os.environ.get("PIN", "111111")
SCOPE = os.environ.get("CREDENTIAL_SCOPE", "crest_work_event_vc_ldp")

# Facts a verifier reads to derive strength. Their presence is the assertion;
# their values come from infra/certify/config/work_events.csv.
PROVENANCE_FACTS = {
    "sourceClass",
    "captureMethod",
    "adapterRef",
    "receivedAt",
    "sourceExposure",
}
# A stored tier freezes a judgement the verifier should be free to make
# differently, and cannot be raised when identity assurance later improves. In a
# signed, offline artefact that is permanent, which is why this list is asserted
# against the credential rather than left to review.
FORBIDDEN = {
    "tier",
    "trustTier",
    "strength",
    "nationalId",
    "national_id",
    "uin",
    "vid",
    "biometrics",
    "face",
    "encodedPhoto",
    "individualId",
}

FAILURES = []


def check(ok, message):
    print(f"[{'PASS' if ok else 'FAIL'}] {message}")
    if not ok:
        FAILURES.append(message)
    return ok


def get_json(url):
    with urllib.request.urlopen(url, timeout=30) as r:
        return json.loads(r.read().decode())


def holder_key():
    """The wallet's key. Generated per run and never stored: a holder key that
    outlives the test would be a credential-binding secret sitting in a repo
    checkout, and this spike does not need one to be long-lived."""
    return ed25519.Ed25519PrivateKey.generate()


def did_jwk(key):
    """did:jwk of an Ed25519 public key. Certify advertises did:jwk as the
    binding method, and the DID *is* the encoded key, so there is nothing to
    publish or resolve."""
    raw = key.public_key().public_bytes(
        serialization.Encoding.Raw, serialization.PublicFormat.Raw
    )
    jwk = {"kty": "OKP", "crv": "Ed25519", "x": b64u(raw)}
    encoded = b64u(json.dumps(jwk, separators=(",", ":"), sort_keys=True).encode())
    return f"did:jwk:{encoded}", jwk


def proof_jwt(key, did, audience, nonce, client_id):
    """OpenID4VCI holder proof. `typ` is load-bearing: without
    `openid4vci-proof+jwt` the issuer is entitled to treat it as an ordinary
    assertion, and binding the credential to the wrong key is a silent failure
    rather than a loud one."""
    header = {"typ": "openid4vci-proof+jwt", "alg": "EdDSA", "kid": f"{did}#0"}
    # `iss`, if present at all, must be the OAuth client id — Certify compares
    # it against the client the access token was issued to. A wallet-ish name
    # here fails verification with no indication that `iss` was the problem.
    payload = {"iss": client_id, "aud": audience, "iat": int(time.time())}
    if nonce:
        payload["nonce"] = nonce
    return jwt.encode(payload, key, algorithm="EdDSA", headers=header)


def multibase_ed25519(value):
    """Decode a z-prefixed base58btc multibase Ed25519 key to its 32 raw bytes.
    The multicodec prefix for an Ed25519 public key is 0xed 0x01."""
    alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
    assert value.startswith("z"), value
    num = 0
    for ch in value[1:]:
        num = num * 58 + alphabet.index(ch)
    raw = num.to_bytes((num.bit_length() + 7) // 8, "big")
    leading = len(value[1:]) - len(value[1:].lstrip("1"))
    raw = b"\x00" * leading + raw
    assert raw[:2] == b"\xed\x01", raw[:2].hex()
    return raw[2:]


def jcs(value):
    """RFC 8785 canonical JSON, enough of it for a credential: sorted keys, no
    insignificant whitespace, and no HTML escaping. Go's encoding/json escapes
    &, < and > by default and RFC 8785 does not — the same trap CREST's own
    signer hit (see pkg/credential/jcs.go)."""
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def verify_data_integrity(credential, public_key_raw):
    """eddsa-jcs-2022: sign sha256(canonical proof config) || sha256(canonical
    document), each without the proofValue."""
    import hashlib

    proof = dict(credential["proof"])
    signature = proof.pop("proofValue")
    document = {k: v for k, v in credential.items() if k != "proof"}
    # The proof configuration carries the DOCUMENT's @context, not its own and
    # not none. Omitting it produces a different hash and a signature that
    # simply does not verify, with nothing to say which half was wrong.
    proof["@context"] = credential["@context"]
    to_sign = hashlib.sha256(jcs(proof)).digest() + hashlib.sha256(jcs(document)).digest()

    alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
    num = 0
    for ch in signature[1:]:
        num = num * 58 + alphabet.index(ch)
    sig = num.to_bytes(64, "big")

    ed25519.Ed25519PublicKey.from_public_bytes(public_key_raw).verify(sig, to_sign)


def main():
    print(f"certify  {CERTIFY}\nesignet  {ESIGNET}\n")

    # 1 ── what the issuer says it will do
    wk = get_json(f"{CERTIFY}/v1/certify/.well-known/openid-credential-issuer")
    configs = wk.get("credential_configurations_supported", {})
    check("WorkEventCredential" in configs, "Certify advertises a WorkEventCredential")
    cfg = configs.get("WorkEventCredential", {})
    check(cfg.get("scope") == SCOPE, f"its scope is {SCOPE}")
    check(bool(wk.get("authorization_servers")), "it names an authorization server")
    credential_endpoint = wk["credential_endpoint"]

    # 2 ── a worker authenticates, and the wallet gets a token for that scope
    refresh_csrf()
    ensure_mock_identity(INDIVIDUAL_ID, PIN)
    key = load_key("wallet")
    client_id = client_id_for("wallet", key)
    register_client(key, client_id, "crest")
    # The scope is the credential scope ALONE. eSignet's OIDCScopeValidator
    # rejects a request that mixes a credential scope with `openid` or any
    # authorize scope, and answers `invalid_scope` without saying which of the
    # two rules was broken. So a VCI authorization yields an access token and no
    # id_token, which is right: the wallet is asking for a credential, not for
    # an assertion about who the worker is.
    tokens = authorize(client_id, key, INDIVIDUAL_ID, PIN, scope=SCOPE)

    claims = jwt.decode(tokens["access_token"], options={"verify_signature": False})
    audience = claims.get("aud")
    audience = audience if isinstance(audience, list) else [audience]
    check(
        any(credential_endpoint.rstrip("/").endswith(a.rstrip("/").split("/")[-1]) or
            a.rstrip("/") == credential_endpoint.rstrip("/") for a in audience if a),
        f"the access token's audience names the credential endpoint ({audience})",
    )

    # 3 ── the wallet proves it holds a key, and asks for the credential
    hk = holder_key()
    did, _ = did_jwk(hk)
    proof = proof_jwt(hk, did, wk["credential_issuer"], tokens.get("c_nonce"), client_id)
    body, _ = post(
        credential_endpoint,
        {
            "format": "ldp_vc",
            "credential_definition": {
                "@context": ["https://www.w3.org/2018/credentials/v1"],
                "type": ["VerifiableCredential", "WorkEventCredential"],
            },
            "proof": {"proof_type": "jwt", "jwt": proof},
        },
        {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {tokens['access_token']}",
        },
    )
    if "credential" not in body:
        raise SystemExit(f"issuance failed: {json.dumps(body)[:2000]}")
    credential = body["credential"]
    print(json.dumps(credential, indent=2)[:2000])

    # Kept when asked for, because the printed-card and offline legs (#66) need
    # a real credential from the deployed issuer, not a hand-authored one. A
    # card rendered from a credential we signed ourselves would prove the
    # renderer and nothing else.
    if os.environ.get("CREDENTIAL_OUT"):
        with open(os.environ["CREDENTIAL_OUT"], "w", encoding="utf-8") as fh:
            json.dump(credential, fh, indent=2)
        print(f"[kept] credential written to {os.environ['CREDENTIAL_OUT']}")

    subject = credential.get("credentialSubject", {})
    # Certify appends `#0` — did:jwk has exactly one key, and #0 is the only
    # valid reference to it.
    check(
        (subject.get("id") or "").split("#")[0] == did,
        f"the credential is bound to the holder's own key ({subject.get('id')})",
    )

    # 4 ── the issuer resolves, and the signature checks out against it
    issuer = credential["issuer"]
    check(issuer.startswith("did:web:"), f"the issuer is a resolvable DID ({issuer})")
    did_doc = get_json(f"{CERTIFY}/v1/certify/.well-known/did.json")
    check(did_doc["id"] == issuer, "the DID document Certify serves is the credential's issuer")
    raw = multibase_ed25519(did_doc["verificationMethod"][0]["publicKeyMultibase"])
    try:
        verify_data_integrity(credential, raw)
        check(True, "the credential's Ed25519 proof verifies against that key")
    except Exception as exc:  # noqa: BLE001 — the failure text is the finding
        check(False, f"the credential's Ed25519 proof verifies against that key ({exc})")

    # 5 ── facts, not judgements
    provenance = subject.get("provenance", {})
    check(
        PROVENANCE_FACTS.issubset(provenance.keys()),
        f"it carries the provenance facts a verifier derives strength from "
        f"(missing: {sorted(PROVENANCE_FACTS - set(provenance))})",
    )
    flat = json.dumps(credential)
    present = sorted(f for f in FORBIDDEN if f'"{f}"' in flat)
    check(not present, f"it carries no tier and no identifier (found: {present})")

    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
