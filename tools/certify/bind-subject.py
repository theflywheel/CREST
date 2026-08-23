#!/usr/bin/env python3
"""Re-key the work-event fixture by the pairwise subjects a deployment mints.

The CSV data provider looks a work event up by the access token's `sub`. For
eSignet that is a pairwise subject: a value that exists only for this relying
party, at this deployment, and that means nothing anywhere else. That property
is the point — it is what stops a work record being joined to a person across
systems — and it is also why the fixture cannot simply be committed with the
right keys in it.

So: authenticate each fixture worker once, read the subject eSignet hands back,
and write the same rows keyed by it.

    python3 tools/certify/bind-subject.py > work_events.csv

Nothing here persists an identifier. The individualId is a synthetic value that
lives only in the mock identity system, and the only thing written out is the
subject, which is already what the credential will be about.

Usage is normally `make certify-bind`, which also copies the result into the
running container.
"""
import csv
import io
import os
import sys

import jwt

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "spikes"))
from esignet_oidc import (  # noqa: E402
    authorize,
    client_id_for,
    ensure_mock_identity,
    load_key,
    refresh_csrf,
    register_client,
)

FIXTURE = os.path.join(
    os.path.dirname(os.path.abspath(__file__)), "..", "..", "infra", "certify", "config", "work_events.csv"
)
PIN = os.environ.get("PIN", "111111")
RELYING_PARTY = os.environ.get("RELYING_PARTY", "crest")


def main():
    rows = list(csv.DictReader(open(FIXTURE)))
    refresh_csrf()
    key = load_key("wallet")
    client_id = client_id_for("wallet", key)
    register_client(key, client_id, RELYING_PARTY)

    out = []
    for row in rows:
        individual_id = row["individualId"]
        ensure_mock_identity(individual_id, PIN)
        tokens = authorize(client_id, key, individual_id, PIN, scope=os.environ.get(
            "CREDENTIAL_SCOPE", "crest_work_event_vc_ldp"))
        claims = jwt.decode(tokens["access_token"], options={"verify_signature": False})
        subject = claims["sub"]
        print(f"{individual_id} -> {subject}", file=sys.stderr)
        row["individualId"] = subject
        out.append(row)

    buf = io.StringIO()
    writer = csv.DictWriter(buf, fieldnames=list(rows[0].keys()), lineterminator="\n")
    writer.writeheader()
    writer.writerows(out)
    sys.stdout.write(buf.getvalue())


if __name__ == "__main__":
    main()
