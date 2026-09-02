#!/usr/bin/env python3
"""Independently verify an Ed25519Signature2020 WorkEventCredential (#155 C).

A second implementation, deliberately not Inji's: URDNA2015 canonicalisation
via pyld, sha256(proof config) || sha256(document), Ed25519 against the key
in the DID document the issuer itself serves. Proven by tampering as well as
by verifying: a changed outcome value must be rejected, because a JSON-LD
term the context fails to define would be silently dropped from the RDF and
so fall out from under the signature — the exact failure the @vocab line in
apps/contexts/work-event-v1.json exists to prevent.

    python3 tools/spikes/verify-2020.py credential.json [did-document.json]

Needs `pip install pyld base58 cryptography` (not stdlib-only, which is why
this is a separate script from certify-issue.py). CONTEXT_MAP can rewrite a
context origin for a local stack, e.g.
    CONTEXT_MAP="http://apps/=http://localhost:59110/"
"""
import hashlib
import json
import os
import sys
import urllib.request

import base58
from cryptography.hazmat.primitives.asymmetric import ed25519
from pyld import jsonld
from pyld.documentloader import requests as requests_loader

def main():
    if len(sys.argv) < 2:
        raise SystemExit(__doc__)
    cred = json.load(open(sys.argv[1]))

    mapping = os.environ.get("CONTEXT_MAP", "")
    base = requests_loader.requests_document_loader()
    if mapping:
        src, dst = mapping.split("=", 1)
        jsonld.set_document_loader(lambda url, options={}: base(url.replace(src, dst), options))

    if len(sys.argv) > 2:
        did_doc = json.load(open(sys.argv[2]))
    else:
        # did:web:host[:path…] → {scheme}://host/path…/did.json, .well-known
        # when the path is bare; http only for localhost, per the method spec.
        parts = cred["issuer"].removeprefix("did:web:").split(":")
        host = parts[0].replace("%3A", ":")
        path = "/".join(parts[1:]) or ".well-known"
        scheme = "http" if host.startswith("localhost") else "https"
        # The method spec puts did.json directly under the path; Certify
        # serves it under path/.well-known/. Try both, spec first.
        did_doc = None
        for candidate in (f"{scheme}://{host}/{path}/did.json",
                          f"{scheme}://{host}/{path}/.well-known/did.json"):
            try:
                with urllib.request.urlopen(candidate) as r:
                    body = json.load(r)
                if "verificationMethod" in body:
                    did_doc = body
                    break
            except Exception:  # noqa: BLE001 — the next candidate is the handler
                continue
        if did_doc is None:
            raise SystemExit(f"could not resolve {cred['issuer']} to a DID document")

    proof = dict(cred["proof"])
    doc = {k: v for k, v in cred.items() if k != "proof"}
    signature = base58.b58decode(proof.pop("proofValue")[1:])
    proof["@context"] = "https://w3id.org/security/suites/ed25519-2020/v1"

    def canon(x):
        return jsonld.normalize(x, {"algorithm": "URDNA2015", "format": "application/n-quads"})

    to_sign = hashlib.sha256(canon(proof).encode()).digest() + hashlib.sha256(canon(doc).encode()).digest()
    raw = base58.b58decode(did_doc["verificationMethod"][0]["publicKeyMultibase"][1:])
    if raw[:2] == b"\xed\x01":
        raw = raw[2:]
    key = ed25519.Ed25519PublicKey.from_public_bytes(raw)

    key.verify(signature, to_sign)
    print("[PASS] the Ed25519Signature2020 proof verifies (URDNA2015, independent implementation)")

    tampered = json.loads(json.dumps(doc))
    tampered["credentialSubject"]["outcome"]["value"] = 999999
    t = hashlib.sha256(canon(proof).encode()).digest() + hashlib.sha256(canon(tampered).encode()).digest()
    try:
        key.verify(signature, t)
    except Exception:
        print("[PASS] a tampered outcome is rejected — the field is under the signature")
        return 0
    print("[FAIL] a tampered outcome still verifies: the context is dropping terms")
    return 1

if __name__ == "__main__":
    sys.exit(main())
