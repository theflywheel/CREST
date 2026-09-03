#!/usr/bin/env python3
"""Onboard Mimoto as an OIDC client of eSignet, and mint the keystore it needs.

Inji's documentation describes this as an onboarding step done by hand, with a
`oidckeystore.p12` obtained from somewhere and dropped into a certs directory.
That is a private key with no recorded provenance, which is not a thing a
payments-adjacent deployment should have. This does it explicitly instead:
generate the key here, register the matching public key with eSignet, and print
the keystore so the deployment can hold it as a secret.

    python3 tools/certify/mimoto-onboard.py            # prints base64 PKCS#12

The output is a PRIVATE KEY. It is written to stdout so it can be piped into a
secret store and never lands in a file by accident; it must not be committed,
and `make mimoto-onboard` sets it as a Railway variable directly.

The client id is derived from the key, for the same reason the spike's is:
eSignet cannot rotate a registered client's public key, so losing the key means
a new client rather than a client nobody can authenticate as.
"""
import base64
import os
import sys

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives.serialization import pkcs12
from cryptography import x509
from cryptography.x509.oid import NameOID
from datetime import datetime, timedelta, timezone

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "spikes"))
from esignet_oidc import (  # noqa: E402
    REDIRECT_URI,
    b64u,
    client_id_for,
    jwk_of,
    now_iso,
    post,
    refresh_csrf,
    ESIGNET,
)

ALIAS = os.environ.get("MIMOTO_CLIENT_ALIAS", "crest-wallet-client")
RELYING_PARTY = os.environ.get("RELYING_PARTY", "crest")
WALLET_REDIRECT = os.environ.get(
    "WALLET_REDIRECT_URI", "https://crest-inji-web-production.up.railway.app/redirect"
)


def self_signed(key, subject):
    """Mimoto's keystore reader wants a certificate alongside the key. Nothing
    verifies this certificate — eSignet is given the public JWK directly — so it
    exists to satisfy the PKCS#12 format rather than to assert anything."""
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, subject)])
    now = datetime.now(timezone.utc)
    return (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - timedelta(minutes=5))
        .not_valid_after(now + timedelta(days=3650))
        .sign(key, hashes.SHA256())
    )


def register(key, client_id):
    body = {
        "requestTime": now_iso(),
        "request": {
            "clientId": client_id,
            "clientName": client_id,
            "clientNameLangMap": {"eng": client_id},
            "publicKey": jwk_of(key, client_id),
            "relyingPartyId": RELYING_PARTY,
            "userClaims": ["name", "phone_number"],
            "authContextRefs": ["mosip:idp:acr:generated-code", "mosip:idp:acr:static-code"],
            "logoUri": os.environ.get("CREST_LOGO_URL", "https://crest.theflywheel.in/logo.png"),
            # Both: the wallet redirects a browser back to Inji Web, and the
            # spike's loopback URI keeps a scripted flow possible against the
            # same client rather than needing a second one.
            "redirectUris": [WALLET_REDIRECT, REDIRECT_URI],
            "grantTypes": ["authorization_code"],
            "clientAuthMethods": ["private_key_jwt"],
        },
    }
    env, _ = post(f"{ESIGNET}/v1/esignet/client-mgmt/oauth-client", body)
    if env.get("errors"):
        codes = {e.get("errorCode") for e in env["errors"]}
        if codes != {"duplicate_client_id"}:
            raise SystemExit(f"client registration failed: {env['errors']}")
        print("client already registered", file=sys.stderr)


def main():
    password = os.environ.get("MIMOTO_P12_PASSWORD")
    if not password:
        raise SystemExit("set MIMOTO_P12_PASSWORD")
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    client_id = client_id_for("mimoto", key)

    refresh_csrf()
    register(key, client_id)

    blob = pkcs12.serialize_key_and_certificates(
        name=ALIAS.encode(),
        key=key,
        cert=self_signed(key, client_id),
        cas=None,
        encryption_algorithm=serialization.BestAvailableEncryption(password.encode()),
    )
    print(f"client_id={client_id}", file=sys.stderr)
    sys.stdout.write(base64.b64encode(blob).decode())


if __name__ == "__main__":
    main()
