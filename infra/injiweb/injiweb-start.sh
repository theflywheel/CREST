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

# Local compose stages a localhost-URL issuer config as a read-only bind
# mount; sed -i below cannot rename over a single-file mount, so it is copied
# into place first. Absent on Railway, where the baked config is the right one.
if [ -f /home/mosip/local/mimoto-issuers-config.json ]; then
  cp /home/mosip/local/mimoto-issuers-config.json "$CONFIG"
fi

if [ -n "$MIMOTO_OIDC_CLIENT_ID" ]; then
  sed -i "s|CLIENT_ID_PLACEHOLDER|$MIMOTO_OIDC_CLIENT_ID|g" "$CONFIG"
else
  echo "MIMOTO_OIDC_CLIENT_ID is not set; the issuer config still has its placeholder" >&2
fi

# The stock entrypoint (configure_start.sh) writes env.config.js from the
# environment — MIMOTO_URL above all — before starting nginx. Exec'ing nginx
# directly skips that, and the wallet frontend then calls the baked default
# http://localhost:8099, which exists nowhere. Chain through it instead.
exec ./configure_start.sh nginx -g 'daemon off;'
