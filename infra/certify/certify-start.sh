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

DATA_DIR="$(dirname "${CERTIFY_WORK_EVENTS_CSV:-/home/inji/data/work_events.csv}")"
mkdir -p "$DATA_DIR"
if [ ! -f "${CERTIFY_WORK_EVENTS_CSV:-/home/inji/data/work_events.csv}" ]; then
  echo "seeding work events from the image template"
  cp /home/inji/config/work_events.csv "${CERTIFY_WORK_EVENTS_CSV:-/home/inji/data/work_events.csv}"
fi

exec /home/inji/configure_start.sh "$@"
