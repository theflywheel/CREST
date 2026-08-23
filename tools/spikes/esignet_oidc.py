"""Driving eSignet's OIDC flow from a script, without a browser.

Extracted from tools/spikes/esignet-pairwise.py when the Certify spike (#1)
needed the same flow with a different scope. Two copies of this would be two
places to fix the next time eSignet moves a header, and the headers here were
expensive enough to find that keeping them in one place is worth a module.

Everything an implementation choice depends on is documented at the point it
bites; see docs/p0-findings.md for what these cost to discover.

Requires: cryptography, pyjwt.
"""
import base64
import hashlib
import http.cookiejar
import json
import os
import secrets
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone

import jwt
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

ESIGNET = os.environ.get("ESIGNET", "https://crest-esignet-production.up.railway.app")
MOCK_IDENTITY = os.environ.get(
    "MOCK_IDENTITY", "https://crest-mock-identity-production.up.railway.app"
)
KEY_DIR = os.environ.get("RP_KEY_DIR", os.path.expanduser("~/.crest/esignet-rp-keys"))
REDIRECT_URI = os.environ.get("REDIRECT_URI", "http://localhost:8765/callback")


def b64u(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).decode().rstrip("=")


def now_iso() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")


# eSignet's /authorization/** endpoints sit behind Spring CSRF: a session cookie
# plus an X-XSRF-TOKEN header, both minted by /csrf/token. Without them every
# call answers a bare 403 with an empty body and no hint that CSRF is the reason.
JAR = http.cookiejar.CookieJar()
OPENER = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(JAR))
CSRF = {"header": "X-XSRF-TOKEN", "token": ""}


def refresh_csrf():
    with OPENER.open(f"{ESIGNET}/v1/esignet/csrf/token") as r:
        body = json.loads(r.read().decode())
    CSRF["header"], CSRF["token"] = body["headerName"], body["token"]


def post(url, body, headers=None, raw=False):
    """Returns (parsed envelope, raw response text). The raw text matters: the
    oauth-details hash below must be taken over the exact bytes eSignet
    serialised, not over a re-serialisation of the parsed object."""
    data = body if raw else json.dumps(body).encode()
    headers = dict(headers or {"Content-Type": "application/json"})
    if CSRF["token"]:
        headers[CSRF["header"]] = CSRF["token"]
    req = urllib.request.Request(url, data, headers)
    try:
        with OPENER.open(req) as r:
            text = r.read().decode()
            return json.loads(text), text
    except urllib.error.HTTPError as e:
        payload = e.read().decode()
        raise SystemExit(f"{url} -> HTTP {e.code}\n{payload}")


def oauth_details_hash(raw_text):
    """eSignet's HeaderValidationFilter guards /authenticate with two headers:
    `oauth-details-key` (the transaction id) and `oauth-details-hash`. Neither is
    returned by the server — the client is expected to derive the hash itself as
    base64url(sha256(<the response object, verbatim>)), which is what the eSignet
    UI does. Miss it and every call answers `invalid_transaction`, which reads
    like an expired transaction rather than a missing header.

    Hashing a re-serialised copy of the parsed object does not work: it has to be
    the same byte sequence Jackson wrote, key order and spacing included.
    """
    start = raw_text.index('"response":') + len('"response":')
    end = raw_text.rindex(',"errors"')
    return b64u(hashlib.sha256(raw_text[start:end].encode()).digest())


def unwrap(envelope, what):
    """eSignet answers 200 with an `errors` array; a bare status code proves nothing."""
    if envelope.get("errors"):
        raise SystemExit(f"{what} failed: {json.dumps(envelope['errors'])}")
    return envelope["response"]


def load_key(label):
    """One key per client. eSignet uniquely indexes the public key hash, so two
    clients cannot share a keypair — which is right, and worth knowing before a
    pilot writes a single RP key into shared configuration."""
    os.makedirs(KEY_DIR, exist_ok=True)
    path = os.path.join(KEY_DIR, f"{label}.pem")
    if not os.path.exists(path):
        key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        with open(os.open(path, os.O_CREAT | os.O_WRONLY | os.O_TRUNC, 0o600), "wb") as fh:
            fh.write(
                key.private_bytes(
                    serialization.Encoding.PEM,
                    serialization.PrivateFormat.PKCS8,
                    serialization.NoEncryption(),
                )
            )
    with open(path, "rb") as fh:
        return serialization.load_pem_private_key(fh.read(), None)


def client_id_for(label, key):
    """eSignet has no way to rotate a registered client's public key: the client
    record is immutable in that field and the key hash is uniquely indexed. So
    the key decides the client id — lose the key file and you get a new client
    rather than a client you can never authenticate as again."""
    n = key.public_key().public_numbers().n
    digest = hashlib.sha256(str(n).encode()).hexdigest()[:8]
    return f"crest-rp-{label}-{digest}"


def jwk_of(key, kid):
    n = key.public_key().public_numbers()

    def num(i):
        return b64u(i.to_bytes((i.bit_length() + 7) // 8, "big"))

    return {"kty": "RSA", "e": num(n.e), "n": num(n.n), "use": "sig", "alg": "RS256", "kid": kid}


def register_client(key, client_id, relying_party_id):
    """Idempotent: a second run re-registers the same key and eSignet accepts it."""
    body = {
        "requestTime": now_iso(),
        "request": {
            "clientId": client_id,
            "clientName": client_id,
            "clientNameLangMap": {"eng": client_id},
            "publicKey": jwk_of(key, client_id),
            "relyingPartyId": relying_party_id,
            "userClaims": ["name", "phone_number"],
            "authContextRefs": ["mosip:idp:acr:generated-code", "mosip:idp:acr:static-code"],
            "logoUri": "https://crest.example/logo.png",
            "redirectUris": [REDIRECT_URI],
            "grantTypes": ["authorization_code"],
            "clientAuthMethods": ["private_key_jwt"],
        },
    }
    env, _ = post(f"{ESIGNET}/v1/esignet/client-mgmt/oauth-client", body)
    if env.get("errors"):
        codes = {e.get("errorCode") for e in env["errors"]}
        if codes == {"duplicate_client_id"}:
            return  # already registered by an earlier run
        raise SystemExit(f"client registration failed: {json.dumps(env['errors'])}")


def ensure_mock_identity(individual_id, pin, name="Test Worker"):
    """The mock identity system rejects a duplicate individualId, so this is
    idempotent by way of the error it returns. Every value here is invented —
    the rule that no real personal data reaches a fixture applies here too, and
    the individualId never leaves the mock system."""
    one = [{"language": "eng", "value": name.split()[0]}]
    env, _ = post(
        f"{MOCK_IDENTITY}/v1/mock-identity-system/identity",
        {
            "requestTime": now_iso(),
            "request": {
                "individualId": individual_id,
                "pin": pin,
                "fullName": [{"language": "eng", "value": name}],
                "givenName": one,
                "familyName": [{"language": "eng", "value": "Worker"}],
                "middleName": [{"language": "eng", "value": "M"}],
                "nickName": [{"language": "eng", "value": "Testy"}],
                "preferredUsername": [{"language": "eng", "value": "testworker"}],
                "preferredLang": "eng",
                "gender": [{"language": "eng", "value": "Female"}],
                "dateOfBirth": "1990/01/01",
                "streetAddress": [{"language": "eng", "value": "1 Test Road"}],
                "locality": [{"language": "eng", "value": "Testville"}],
                "region": [{"language": "eng", "value": "Test Region"}],
                "postalCode": "560001",
                "country": [{"language": "eng", "value": "IND"}],
                "phone": "+919999999999",
                "email": "test.worker@example.invalid",
                "encodedPhoto": "data:image/jpeg;base64,/9j/4AAQSkZJRg==",
            },
        },
    )
    for e in env.get("errors") or []:
        if "already" not in (e.get("message") or e.get("errorMessage") or ""):
            # stderr: callers pipe stdout (bind-subject.py writes a CSV to it).
            print(f"note: mock identity not created ({e}); continuing in case it exists", file=sys.stderr)
        break


def authorize(client_id, key, individual_id, pin, scope="openid profile"):
    """One full authorization-code exchange. Returns the whole token response —
    the id_token for #3's question about `sub`, the access_token for #1's, which
    is what a credential endpoint actually wants."""
    nonce, state = secrets.token_urlsafe(16), secrets.token_urlsafe(16)
    verifier = b64u(secrets.token_bytes(32))
    challenge = b64u(hashlib.sha256(verifier.encode()).digest())

    env, raw = post(
        f"{ESIGNET}/v1/esignet/authorization/v3/oauth-details",
        {
            "requestTime": now_iso(),
            "request": {
                "clientId": client_id,
                "scope": scope,
                "responseType": "code",
                "redirectUri": REDIRECT_URI,
                "display": "page",
                "prompt": "consent",
                "acrValues": "mosip:idp:acr:static-code",
                "nonce": nonce,
                "state": state,
                "codeChallenge": challenge,
                "codeChallengeMethod": "S256",
            },
        },
    )
    details = unwrap(env, "oauth-details")
    txn = details["transactionId"]
    bind = {
        "Content-Type": "application/json",
        "oauth-details-key": txn,
        "oauth-details-hash": oauth_details_hash(raw),
    }

    env, _ = post(
        f"{ESIGNET}/v1/esignet/authorization/v3/authenticate",
        {
            "requestTime": now_iso(),
            "request": {
                "transactionId": txn,
                "individualId": individual_id,
                "challengeList": [
                    {"authFactorType": "PIN", "challenge": pin, "format": "number"}
                ],
            },
        },
        bind,
    )
    auth = unwrap(env, "authenticate")
    # v3/authenticate rotates the transaction id; the rest of the flow uses the
    # new one, while the hash header stays bound to the original oauth-details.
    txn = auth["transactionId"]
    bind["oauth-details-key"] = txn

    env, _ = post(
        f"{ESIGNET}/v1/esignet/authorization/auth-code",
        {
            "requestTime": now_iso(),
            "request": {
                "transactionId": txn,
                "acceptedClaims": details.get("voluntaryClaims", []),
                # A credential scope arrives here as an *authorize* scope the
                # user is consenting to. Dropping it produces a token without
                # the credential scope, and the failure surfaces three calls
                # later at the credential endpoint as an authorisation error.
                "permittedAuthorizeScopes": details.get("authorizeScopes", []),
            },
        },
        bind,
    )
    code = unwrap(env, "auth-code")["code"]

    token_endpoint = f"{ESIGNET}/v1/esignet/oauth/v2/token"
    assertion = jwt.encode(
        {
            "iss": client_id,
            "sub": client_id,
            "aud": token_endpoint,
            "jti": secrets.token_urlsafe(16),
            "exp": int(time.time()) + 300,
            "iat": int(time.time()),
        },
        key,
        algorithm="RS256",
        headers={"kid": client_id},
    )
    form = urllib.parse.urlencode(
        {
            "grant_type": "authorization_code",
            "code": code,
            "redirect_uri": REDIRECT_URI,
            "client_id": client_id,
            "client_assertion_type": "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
            "client_assertion": assertion,
            "code_verifier": verifier,
        }
    ).encode()
    tokens, _ = post(
        token_endpoint,
        form,
        {"Content-Type": "application/x-www-form-urlencoded"},
        raw=True,
    )
    if "access_token" not in tokens:
        raise SystemExit(f"token exchange failed: {json.dumps(tokens)}")
    return tokens
