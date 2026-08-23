#!/bin/sh
# Inji Web's entrypoint.
#
# It exists to substitute the OIDC client id into the issuer config before nginx
# serves it. That document is not only read by the browser: Mimoto fetches the
# same URL at startup, so a placeholder left here reaches the wallet's backend
# too and the token exchange fails with `invalid_client` several redirects into
# a flow that gives no hint where the value came from.
#
# The client id is derived from a key registered with eSignet at deploy time
# (tools/certify/mimoto-onboard.py), so it cannot be known when the image is
# built.
set -e

CONFIG=/home/mosip/mimoto-issuers-config.json

if [ -n "$MIMOTO_OIDC_CLIENT_ID" ]; then
  sed -i "s|CLIENT_ID_PLACEHOLDER|$MIMOTO_OIDC_CLIENT_ID|g" "$CONFIG"
else
  echo "MIMOTO_OIDC_CLIENT_ID is not set; the issuer config still has its placeholder" >&2
fi

exec nginx -g 'daemon off;'
