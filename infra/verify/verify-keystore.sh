#!/bin/sh
# Generate the Inji Verify signing keystore CREST holds (#65, p0 finding V1).
#
# Why this exists: mosipid/inji-verify-service:0.16.0 signs the OpenID4VP
# authorization requests it hands to a wallet with a PKCS#12 that ships inside
# the public image — `BOOT-INF/classes/sample-keystore/test.p12`, alias `test`,
# password `mosip`, all three published. Anyone who can `docker pull` holds the
# key, so a signature over a verifier request proves nothing about which
# verifier made it. This script mints a key that never leaves the deployment.
#
# What the service actually requires of the file (read out of the 0.16.0 jar,
# io.inji.verify.key.impl.P12KeyExtractor):
#
#   * PKCS#12, opened with `inji.keystore.file.pass`.
#   * The alias is NOT fixed. The extractor enumerates aliases and takes the
#     first key entry whose certificate's public key algorithm is `Ed25519` or
#     `EdDSA`; anything else and it throws
#     `No EdDSA key entry found in the P12 file.`
#   * So a keystore made with keytool's default algorithm (RSA) — the obvious
#     thing to try, and what broke start-up when V1 was recorded — fails on the
#     key type, not on the alias or the path.
#   * The key password must equal the store password: the extractor calls
#     KeyStore.getKey(alias, storePassword.toCharArray()).
#
# The path is `inji.keystore.file.path`, a Spring Resource string, so a
# `file:` URL outside the jar is accepted. Both properties are @Value-injected
# and therefore settable as INJI_KEYSTORE_FILE_PATH / INJI_KEYSTORE_FILE_PASS.
#
# openssl rather than keytool so this runs without a JDK on the machine.
set -eu

OUT_DIR="${VERIFY_KEYSTORE_DIR:-infra/verify/secrets}"
OUT="${VERIFY_KEYSTORE_FILE:-$OUT_DIR/verify-signing.p12}"
ALIAS="${VERIFY_KEYSTORE_ALIAS:-crest-verify}"
SUBJECT="${VERIFY_KEYSTORE_SUBJECT:-/CN=crest-verify/O=CREST}"
DAYS="${VERIFY_KEYSTORE_DAYS:-3650}"

# compose reads infra/compose/.env for the same variable, so honour it here
# rather than making the operator export it twice and get them out of step.
ENV_FILE="${VERIFY_ENV_FILE:-infra/compose/.env}"
if [ -z "${INJI_VERIFY_KEYSTORE_PASSWORD:-}" ] && [ -f "$ENV_FILE" ]; then
  INJI_VERIFY_KEYSTORE_PASSWORD="$(sed -n 's/^INJI_VERIFY_KEYSTORE_PASSWORD=//p' "$ENV_FILE" | head -1)"
fi

if [ -z "${INJI_VERIFY_KEYSTORE_PASSWORD:-}" ]; then
  echo "INJI_VERIFY_KEYSTORE_PASSWORD is not set." >&2
  echo "Set it to a value this deployment generated and keeps in its secret store." >&2
  echo "  export INJI_VERIFY_KEYSTORE_PASSWORD=\"\$(openssl rand -base64 24)\"" >&2
  echo "or put it in $ENV_FILE, which compose reads too." >&2
  exit 2
fi

# The one value that is definitely not a secret. Refusing it here is the whole
# point of the change: a keystore CREST generated under the published password
# is no better than the published keystore.
if [ "$INJI_VERIFY_KEYSTORE_PASSWORD" = "mosip" ]; then
  echo "INJI_VERIFY_KEYSTORE_PASSWORD is 'mosip', the password published in the" >&2
  echo "upstream image. Refusing — see docs/p0-findings.md V1." >&2
  exit 2
fi

if [ -f "$OUT" ] && [ "${VERIFY_KEYSTORE_FORCE:-0}" != "1" ]; then
  echo "$OUT already exists; leaving it alone."
  echo "Rotating the key invalidates any request already signed with the old one."
  echo "Set VERIFY_KEYSTORE_FORCE=1 to overwrite deliberately."
  exit 0
fi

mkdir -p "$(dirname "$OUT")"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

openssl genpkey -algorithm ed25519 -out "$TMP/key.pem" >/dev/null 2>&1
openssl req -new -x509 -key "$TMP/key.pem" -out "$TMP/cert.pem" \
  -days "$DAYS" -subj "$SUBJECT" >/dev/null 2>&1
openssl pkcs12 -export \
  -inkey "$TMP/key.pem" -in "$TMP/cert.pem" \
  -name "$ALIAS" -out "$OUT" \
  -passout "pass:$INJI_VERIFY_KEYSTORE_PASSWORD" >/dev/null 2>&1
chmod 0600 "$OUT"

echo "wrote $OUT"
echo "  alias:       $ALIAS"
echo "  algorithm:   Ed25519 (what P12KeyExtractor requires)"
echo "  fingerprint: $(openssl x509 -in "$TMP/cert.pem" -noout -fingerprint -sha256 | cut -d= -f2)"
# The did.json the service publishes carries this key in base58btc multibase.
# Printing the raw 32 bytes here is what makes the proof in docs/DEPLOYMENT.md
# a comparison rather than an assertion.
echo "  public key (hex): $(openssl pkey -in "$TMP/key.pem" -pubout -outform DER \
  | tail -c 32 | od -An -tx1 | tr -d ' \n')"
echo
echo "For a deployment with no bind mounts (Railway), pass it as a secret:"
echo "  INJI_VERIFY_KEYSTORE_P12_B64=\$(base64 < $OUT | tr -d '\\n')"
