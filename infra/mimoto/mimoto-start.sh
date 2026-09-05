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

# The keystore has to outlive the container. It is not only the OIDC client key:
# the MOSIP key manager generates its ROOT master key *into this same p12* on
# first start and records the alias in the database. A container that comes back
# with a freshly materialised keystore therefore finds a DB row naming an alias
# the file does not contain, and dies with `No such alias` — the same failure
# eSignet had, one layer down. So the keystore lives on a volume and is written
# from the secret only when it is not already there.
#
# Railway creates the volume's mount point owned by root while mimoto runs as
# 1002:1001, so the service starts as root (RAILWAY_RUN_UID=0), takes ownership
# and drops straight back.
if [ "$(id -u)" = "0" ]; then
  mkdir -p "$CERTS_DIR"
  chown -R 1002:1001 "$CERTS_DIR"
  # BusyBox's setpriv has no --reuid, so `su` it is. PATH is restated because
  # `su` rebuilds it from login defaults and java would not be found.
  exec su mosip -s /bin/sh -c \
    'export PATH=/opt/java/openjdk/bin:$PATH; exec "$@"' -- sh "$0" "$@"
fi

mkdir -p "$CERTS_DIR"

if [ -f "$CERTS_DIR/oidckeystore.p12" ]; then
  echo "keystore already present on the volume; leaving it alone"
elif [ -n "$MIMOTO_OIDC_P12_B64" ]; then
  echo "$MIMOTO_OIDC_P12_B64" | base64 -d > "$CERTS_DIR/oidckeystore.p12"
  chmod 0600 "$CERTS_DIR/oidckeystore.p12"
else
  echo "MIMOTO_OIDC_P12_B64 is not set; mimoto will fail to start" >&2
fi

# Local compose stages a localhost-URL issuer config as a read-only bind
# mount; sed -i below cannot rename over a single-file mount, so it is copied
# into place first. Absent on Railway, where the baked config is the right one.
if [ -f "$CONFIG_DIR/local/mimoto-issuers-config.json" ]; then
  cp "$CONFIG_DIR/local/mimoto-issuers-config.json" "$CONFIG_DIR/mimoto-issuers-config.json"
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
