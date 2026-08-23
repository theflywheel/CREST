#!/bin/sh
# Mimoto's entrypoint.
#
# POSIX sh, not bash: the mimoto image is Alpine and has no /bin/bash. An
# ENTRYPOINT naming one exits the container instantly, and Railway reports a
# failed deploy with no logs at all — the build succeeds, the image pushes, and
# nothing says why.
#
# It materialises the OIDC keystore from a secret and substitutes the client id
# into the issuer config. Both exist because the keystore is a private key: it
# is registered with eSignet by tools/certify/mimoto-onboard.py, held as a
# deployment secret, and must never be a file in the repository or a layer in an
# image somebody can pull.
set -e

CONFIG_DIR=/home/mosip
CERTS_DIR="$CONFIG_DIR/certs"
mkdir -p "$CERTS_DIR"

if [ -n "$MIMOTO_OIDC_P12_B64" ]; then
  echo "$MIMOTO_OIDC_P12_B64" | base64 -d > "$CERTS_DIR/oidckeystore.p12"
  chmod 0400 "$CERTS_DIR/oidckeystore.p12"
else
  echo "MIMOTO_OIDC_P12_B64 is not set; mimoto will fail to start" >&2
fi

if [ -n "$MIMOTO_OIDC_CLIENT_ID" ]; then
  # The client id is derived from the key that was registered, so it is not
  # knowable when the image is built. A placeholder left unsubstituted would
  # produce an `invalid_client` at the token endpoint, three redirects into a
  # flow, with nothing pointing back here.
  sed -i "s|CLIENT_ID_PLACEHOLDER|$MIMOTO_OIDC_CLIENT_ID|g" \
    "$CONFIG_DIR/mimoto-issuers-config.json"
else
  echo "MIMOTO_OIDC_CLIENT_ID is not set; the issuer config still has its placeholder" >&2
fi

exec /__cacert_entrypoint.sh "$@"
