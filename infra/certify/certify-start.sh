#!/bin/bash
# Certify's entrypoint, wrapping the upstream one.
#
# It does one thing: make sure the work-event CSV exists on the data volume
# before Certify reads it. The file cannot be baked into the image because it is
# keyed by the pairwise subject eSignet mints for this deployment, and that value
# does not exist until a worker has authenticated once. The image carries a
# template; `make certify-bind-subject` replaces it with the real thing.
#
# Seeding only when the file is ABSENT is deliberate. Overwriting on every boot
# would silently undo a binding at the next redeploy, and the symptom —
# ERROR_FETCHING_IDENTITY_DATA — says nothing about a file having been replaced.
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
  # -m preserves the environment. Without it `su` resets PATH and the very
  # next line fails with `exec: java: not found`, which reads as a broken
  # image rather than as a dropped variable.
  exec su -m inji -s /bin/bash -c 'exec "$@"' -- sh "$0" "$@"
fi

DATA_DIR="$(dirname "${CERTIFY_WORK_EVENTS_CSV:-/home/inji/data/work_events.csv}")"
mkdir -p "$DATA_DIR"
if [ ! -f "${CERTIFY_WORK_EVENTS_CSV:-/home/inji/data/work_events.csv}" ]; then
  echo "seeding work events from the image template"
  cp /home/inji/config/work_events.csv "${CERTIFY_WORK_EVENTS_CSV:-/home/inji/data/work_events.csv}"
fi

exec /home/inji/configure_start.sh "$@"
