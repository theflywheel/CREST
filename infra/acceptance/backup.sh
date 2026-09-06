#!/usr/bin/env bash
set -euo pipefail
umask 077

usage() {
  cat >&2 <<'EOF'
usage: backup.sh --project PROJECT --output DIRECTORY

The named Compose project must already be quiesced. This command never stops
containers. The output directory must not exist.
EOF
  exit 2
}

PROJECT=""
OUTPUT=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --project) [ "$#" -ge 2 ] || usage; PROJECT="$2"; shift 2 ;;
    --output) [ "$#" -ge 2 ] || usage; OUTPUT="$2"; shift 2 ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
done

[ -n "$PROJECT" ] && [ -n "$OUTPUT" ] || usage
[[ "$PROJECT" =~ ^[A-Za-z0-9][A-Za-z0-9_-]*$ ]] || { echo "backup: invalid project name" >&2; exit 2; }
[ ! -e "$OUTPUT" ] || { echo "backup: output already exists; refusing overwrite" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "backup: docker is required" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "backup: Docker is unavailable" >&2; exit 1; }

VOLUMES=(pgdata objectdata vaultdata dedidata)
volume_name() { printf '%s_%s' "$PROJECT" "$1"; }

# A physical volume copy is consistent only while no container has the volume
# open. The script deliberately refuses to quiesce the project itself: doing
# that could stop a different operator's stack by accident.
for volume in "${VOLUMES[@]}"; do
  name="$(volume_name "$volume")"
  docker volume inspect "$name" >/dev/null 2>&1 || {
    echo "backup: required volume $name does not exist" >&2
    exit 1
  }
  if [ -n "$(docker ps -q --filter "volume=$name")" ]; then
    echo "backup: volume $name is in use; quiesce the project first" >&2
    exit 1
  fi
done

STAGE="$OUTPUT.tmp.$$"
[ ! -e "$STAGE" ] || { echo "backup: temporary staging path already exists; refusing overwrite" >&2; exit 1; }
cleanup() { rm -rf "$STAGE"; }
trap cleanup EXIT INT TERM
mkdir -m 700 -p "$STAGE/volumes" "$STAGE/config/private"

# Keep the bundle self-describing without recording credentials in a log.
cat >"$STAGE/metadata" <<EOF
format=crest-acceptance-backup-v1
source_project=$PROJECT
EOF

for volume in "${VOLUMES[@]}"; do
  name="$(volume_name "$volume")"
  if [ -n "$(docker ps -q --filter "volume=$name")" ]; then
    echo "backup: volume $name became active; quiesce the project again" >&2
    exit 1
  fi
  echo "backup: archiving $volume"
  docker run --rm --mount "type=volume,src=$name,dst=/source,readonly" alpine:3.20 \
    tar -C /source -cf - . >"$STAGE/volumes/$volume.tar"
  chmod 600 "$STAGE/volumes/$volume.tar"
done

# These files are deployment material rather than database rows. Copy them so
# a restore retains Vault's initialization/key material and the S3 credentials,
# while preserving the private file mode. Do not print their contents.
ACCEPTANCE_DIR="$(dirname "$0")"
for source in "$ACCEPTANCE_DIR"/.env.*; do
  [ -f "$source" ] || continue
  [ ! -L "$source" ] || { echo "backup: environment files may not be symlinks" >&2; exit 1; }
  cp "$source" "$STAGE/config/$(basename "$source")"
done
[ -n "$(find "$STAGE/config" -maxdepth 1 -type f -name '.env.*' -print -quit)" ] || {
  echo "backup: no acceptance environment files found" >&2
  exit 1
}
for file in "$ACCEPTANCE_DIR"/vault.hcl; do
  [ -f "$file" ] || { echo "backup: missing acceptance/vault.hcl" >&2; exit 1; }
  cp "$file" "$STAGE/config/vault.hcl"
done
for source in "$ACCEPTANCE_DIR"/private/*; do
  [ -f "$source" ] || continue
  [ ! -L "$source" ] || { echo "backup: private key files may not be symlinks" >&2; exit 1; }
  cp "$source" "$STAGE/config/private/$(basename "$source")"
done
[ -n "$(find "$STAGE/config/private" -maxdepth 1 -type f -print -quit)" ] || {
  echo "backup: no acceptance private key material found" >&2
  exit 1
}
chmod 600 "$STAGE/config"/.env.* "$STAGE/config/private"/*
chmod 644 "$STAGE/config/vault.hcl"

digest() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi
}
{
  printf '%s  %s\n' "$(digest "$STAGE/metadata")" metadata
  find "$STAGE/volumes" "$STAGE/config" -type f -print | sort | while read -r file; do
    relative="${file#"$STAGE/"}"
    printf '%s  %s\n' "$(digest "$file")" "$relative"
  done
} >"$STAGE/manifest.sha256"
chmod 600 "$STAGE/metadata" "$STAGE/manifest.sha256"
mv "$STAGE" "$OUTPUT"
trap - EXIT INT TERM
echo "backup: complete"
