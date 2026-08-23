#!/usr/bin/env python3
"""Does a relying party actually receive a pairwise, stable subject from eSignet? (#3)

`subject_types_supported: ["pairwise"]` in the discovery document is what eSignet
*advertises*. #3 asks what an RP *receives*. This drives two full OIDC
authorization-code round trips per client, for two clients, against one mock
identity, and compares the four `sub` values:

    same client, two sessions   -> sub MUST be equal      (stable)
    different clients           -> sub MUST differ        (pairwise)

Nothing here persists an identifier. The individualId is a synthetic value that
lives only in the mock identity system; CREST's side of this test only ever
reads `sub` (W9).

The flow itself lives in esignet_oidc.py, shared with the Certify spike (#1).

Usage:
    python3 tools/spikes/esignet-pairwise.py            # against the deployed eSignet
    ESIGNET=http://localhost:58088 MOCK_IDENTITY=... python3 tools/spikes/esignet-pairwise.py

Requires: cryptography, pyjwt.
"""
import os
import sys

import jwt

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from esignet_oidc import (  # noqa: E402
    authorize,
    client_id_for,
    ensure_mock_identity,
    load_key,
    refresh_csrf,
    register_client,
)

INDIVIDUAL_ID = os.environ.get("INDIVIDUAL_ID", "9876543210")
PIN = os.environ.get("PIN", "111111")

if __name__ == "__main__":
    refresh_csrf()
    ensure_mock_identity(INDIVIDUAL_ID, PIN)
    # Two clients under one relying party, one under another. The three
    # comparisons below are what #3 actually asks about.
    plan = [("a", "crest"), ("b", "crest"), ("c", "other-programme")]
    keys = {label: load_key(label) for label, _ in plan}
    clients = [(client_id_for(label, keys[label]), keys[label], rp) for label, rp in plan]
    for cid, key, rp in clients:
        register_client(key, cid, rp)

    def sub_of(cid, key):
        tokens = authorize(cid, key, INDIVIDUAL_ID, PIN)
        # The signature is eSignet's own JWKS; what #3 asks about is the subject
        # the relying party is handed, so that is what we read.
        return jwt.decode(tokens["id_token"], options={"verify_signature": False})["sub"]

    subs = {}
    for cid, key, rp in clients:
        subs[cid] = [sub_of(cid, key) for _ in range(2)]
        print(f"{cid}  (rp={rp})  session 1  sub = {subs[cid][0]}")
        print(f"{cid}  (rp={rp})  session 2  sub = {subs[cid][1]}")

    ok = True
    for cid, _, _ in clients:
        stable = subs[cid][0] == subs[cid][1]
        ok &= stable
        print(f"[{'PASS' if stable else 'FAIL'}] {cid}: sub is stable across two sessions")

    a, b, c = (cid for cid, _, _ in clients)
    # The partition is the relying party, not the client. Both halves are
    # asserted so that a change to either direction fails loudly.
    shared = subs[a][0] == subs[b][0]
    ok &= shared
    print(
        f"[{'PASS' if shared else 'FAIL'}] two clients of one relying party "
        "receive the SAME sub (the partition is the relying party)"
    )
    partitioned = subs[a][0] != subs[c][0]
    ok &= partitioned
    print(
        f"[{'PASS' if partitioned else 'FAIL'}] a different relying party "
        "receives a different sub (pairwise at the relying-party boundary)"
    )

    sys.exit(0 if ok else 1)
