#!/bin/bash
# Certify's entrypoint, wrapping the upstream one.
#
# It does one thing: make sure the work-event CSV is in place before Certify
# reads it. The file cannot be baked into the image because it is keyed by the
# pairwise subject eSignet mints for this deployment, and that value does not
# exist until a worker has authenticated once.
set -e

CSV="${CERTIFY_WORK_EVENTS_CSV:-/home/inji/data/work_events.csv}"

# Railway creates a volume's mount point owned by root, and Certify runs as uid
# 1001, so the first thing it does on a fresh volume is fail to write to it. The
# service therefore starts as root (RAILWAY_RUN_UID=0), takes ownership, and
# drops straight back to 1001 — the issuer's signing key lives on that volume,
# and a process that can be exploited into reading it should not also be root.
if [ "$(id -u)" = "0" ]; then
  mkdir -p "$(dirname "$CSV")" "$(dirname "${CERTIFY_KEYSTORE_PATH:-/home/inji/CERTIFY_PKCS12/local.p12}")"
  chown -R 1001:1001 "$(dirname "$CSV")" "$(dirname "${CERTIFY_KEYSTORE_PATH:-/home/inji/CERTIFY_PKCS12/local.p12}")"
  # PATH is restated rather than inherited: `su` rebuilds it from login
  # defaults even with -m, and the failure is `exec: java: not found`,
  # which reads as a broken image rather than as a dropped variable.
  exec su inji -s /bin/bash -c \
    'export PATH=/opt/java/openjdk/bin:$PATH; exec "$@"' -- sh "$0" "$@"
fi

mkdir -p "$(dirname "$CSV")"

# Three sources, most specific first.
#
# CERTIFY_WORK_EVENTS_B64 is how a deployment supplies the real fixture: the
# file is keyed by the pairwise subjects eSignet minted for THIS deployment, so
# it cannot be built at image-build time and there is no shell on a Railway
# container to write it with. A variable is the platform's own mechanism for
# "deployment-specific content", and setting it redeploys, which is exactly the
# restart the CSV needs — the plugin reads the file once at startup, not per
# request.
if [ -n "$CERTIFY_WORK_EVENTS_B64" ]; then
  echo "writing work events from CERTIFY_WORK_EVENTS_B64"
  echo "$CERTIFY_WORK_EVENTS_B64" | base64 -d > "$CSV"
elif [ ! -f "$CSV" ]; then
  # Seeding only when the file is ABSENT is deliberate: overwriting on every
  # boot would silently undo a binding written onto the volume, and the symptom
  # — ERROR_FETCHING_IDENTITY_DATA — says nothing about a replaced file.
  echo "seeding work events from the image template"
  cp /home/inji/config/work_events.csv "$CSV"
fi

exec /home/inji/configure_start.sh "$@"
