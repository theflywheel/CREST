#!/bin/sh
# Inji Verify's entrypoint (#65, p0 finding V1).
#
# Its only job is to make sure the service signs with the keystore CREST
# generated, and to refuse to start if it would fall back to the one published
# in the image. POSIX sh: the base image is Alpine-flavoured and the upstream
# CMD is already /bin/sh.
set -e

KEYSTORE_DIR=/home/mosip/keystore
KEYSTORE="$KEYSTORE_DIR/verify-signing.p12"

if [ "$(id -u)" = "0" ]; then
  mkdir -p "$KEYSTORE_DIR"
  chown -R 1002:1001 "$KEYSTORE_DIR" 2>/dev/null || true
  exec su mosip -s /bin/sh -c \
    'export PATH=/opt/java/openjdk/bin:$PATH; exec "$@"' -- sh "$0" "$@"
fi

if [ -f "$KEYSTORE" ]; then
  # Compose bind-mounts it read-only; nothing to do.
  echo "verify: signing keystore present at $KEYSTORE"
elif [ -n "${INJI_VERIFY_KEYSTORE_P12_B64:-}" ]; then
  # Railway has no bind mounts. Unlike Mimoto's, this keystore holds no
  # key-manager ROOT alias recorded in a database, so it does not need a
  # volume — re-materialising the same secret is a complete restore.
  mkdir -p "$KEYSTORE_DIR"
  echo "$INJI_VERIFY_KEYSTORE_P12_B64" | base64 -d > "$KEYSTORE"
  chmod 0600 "$KEYSTORE"
  echo "verify: signing keystore materialised from INJI_VERIFY_KEYSTORE_P12_B64"
else
  echo "verify: no signing keystore. Set INJI_VERIFY_KEYSTORE_P12_B64 or mount" >&2
  echo "        one at $KEYSTORE (make verify-keystore)." >&2
  echo "        Refusing to start on the image's published test key — p0 V1." >&2
  exit 1
fi

if [ -z "${INJI_KEYSTORE_FILE_PASS:-}" ] || [ "$INJI_KEYSTORE_FILE_PASS" = "mosip" ]; then
  echo "verify: INJI_KEYSTORE_FILE_PASS is unset or is the published default." >&2
  exit 1
fi

# Belt and braces: even if the environment forgot to point at the file, the
# service must not silently pick the classpath keystore back up.
INJI_KEYSTORE_FILE_PATH="${INJI_KEYSTORE_FILE_PATH:-file:$KEYSTORE}"
export INJI_KEYSTORE_FILE_PATH

exec /__cacert_entrypoint.sh "$@"
