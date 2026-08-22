#!/usr/bin/env python3
"""Derive a DeDi node's checkpoint verifier key, and cross-check it.

In production a verifier key arrives out of band — that is what makes it a
trust root, and fetching it from the node you are verifying proves nothing on
its own. This exists for nodes that mint their own key, where the alternative
is copying a string out of a log by hand.

What makes it worth more than a copy-paste is the cross-check: the key the
node's published manifest advertises must be the key that actually signed the
current checkpoint. The manifest is regenerated on a schedule, so immediately
after a key change it still advertises the previous key — a mismatch that
otherwise shows up as "signature does not verify" with no clue why.

    ./tools/spikes/dedi-verifier-key.py https://node.example/
"""
import base64
import hashlib
import json
import sys
import urllib.request

ALG = b"\x01"  # Ed25519, in the signed-note key format


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        return 2
    base = sys.argv[1].rstrip("/")

    checkpoint = urllib.request.urlopen(base + "/dedi/log/checkpoint").read().decode()
    signature_lines = [l for l in checkpoint.strip().split("\n") if l.startswith("—")]
    if not signature_lines:
        print("checkpoint carries no signature line", file=sys.stderr)
        return 1
    fields = signature_lines[0].split()
    name, signed_by = fields[1], base64.b64decode(fields[2])[:4]

    manifest = json.load(urllib.request.urlopen(base + "/.well-known/dedi.index.json"))
    for key in manifest.get("keys", []):
        raw = key["x"]
        pub = base64.urlsafe_b64decode(raw + "=" * (-len(raw) % 4))
        digest = hashlib.sha256(name.encode() + b"\n" + ALG + pub).digest()[:4]
        if digest == signed_by:
            print(name + "+" + digest.hex() + "+" + base64.b64encode(ALG + pub).decode())
            return 0

    print(
        "no key in the node's manifest signed the current checkpoint.\n"
        "Either the manifest is stale (it is regenerated on a schedule, so this\n"
        "is expected for a minute or so after a key change), or the log is signed\n"
        "by a key the node does not publish — which is a finding, not a retry.",
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
