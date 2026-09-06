#!/usr/bin/env bash
set -euo pipefail
umask 077

usage() {
  cat >&2 <<'EOF'
usage: restore.sh --bundle DIRECTORY --project PROJECT --config-dir EMPTY_DIRECTORY

The target project must not have existing containers or volumes. This command
never starts or stops Compose services.
EOF
  exit 2
}

BUNDLE=""
PROJECT=""
CONFIG_DIR=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bundle) [ "$#" -ge 2 ] || usage; BUNDLE="$2"; shift 2 ;;
    --project) [ "$#" -ge 2 ] || usage; PROJECT="$2"; shift 2 ;;
    --config-dir) [ "$#" -ge 2 ] || usage; CONFIG_DIR="$2"; shift 2 ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
done

[ -n "$BUNDLE" ] && [ -n "$PROJECT" ] && [ -n "$CONFIG_DIR" ] || usage
[[ "$PROJECT" =~ ^[A-Za-z0-9][A-Za-z0-9_-]*$ ]] || { echo "restore: invalid project name" >&2; exit 2; }
[ -d "$BUNDLE" ] || { echo "restore: bundle must be a directory" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "restore: docker is required" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "restore: Docker is unavailable" >&2; exit 1; }

for path in metadata manifest.sha256 volumes/pgdata.tar volumes/objectdata.tar volumes/vaultdata.tar volumes/dedidata.tar config/vault.hcl; do
  [ -f "$BUNDLE/$path" ] || { echo "restore: bundle is missing $path" >&2; exit 1; }
done
[ -n "$(find "$BUNDLE/config" -maxdepth 1 -type f -name '.env.*' -print -quit)" ] || { echo "restore: bundle has no environment files" >&2; exit 1; }
[ -n "$(find "$BUNDLE/config/private" -maxdepth 1 -type f -print -quit)" ] || { echo "restore: bundle has no private key material" >&2; exit 1; }
if [ -n "$(find "$BUNDLE" -type l -print -quit)" ]; then
  echo "restore: symlinks are not allowed in a backup bundle" >&2
  exit 1
fi

format="$(awk -F= '$1 == "format" {print $2}' "$BUNDLE/metadata")"
[ "$format" = "crest-acceptance-backup-v1" ] || { echo "restore: unsupported bundle format" >&2; exit 1; }
python3 "$(dirname "$0")/verify_bundle.py" "$BUNDLE"

digest() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi
}
while read -r expected path; do
  [ -n "$expected" ] && [ -n "$path" ] || continue
  case "$path" in
    metadata|volumes/pgdata.tar|volumes/objectdata.tar|volumes/vaultdata.tar|volumes/dedidata.tar|config/.env.*|config/vault.hcl|config/private/*) ;;
    *) echo "restore: manifest contains an unexpected path" >&2; exit 1 ;;
  esac
  case "$path" in
    */*/*/*) echo "restore: manifest contains a nested path" >&2; exit 1 ;;
  esac
  actual="$(digest "$BUNDLE/$path")"
  [ "$actual" = "$expected" ] || { echo "restore: checksum mismatch for $path" >&2; exit 1; }
done <"$BUNDLE/manifest.sha256"

[ ! -L "$CONFIG_DIR" ] || { echo "restore: config target may not be a symlink" >&2; exit 1; }
[ ! -e "$CONFIG_DIR" ] || {
  [ -d "$CONFIG_DIR" ] && [ -z "$(find "$CONFIG_DIR" -mindepth 1 -print -quit)" ] || {
    echo "restore: config target must be a new or empty directory; refusing overwrite" >&2
    exit 1
  }
}

VOLUMES=(pgdata objectdata vaultdata dedidata)
volume_name() { printf '%s_%s' "$PROJECT" "$1"; }
for volume in "${VOLUMES[@]}"; do
  name="$(volume_name "$volume")"
  if docker volume inspect "$name" >/dev/null 2>&1; then
    echo "restore: target volume $name already exists; refusing overwrite" >&2
    exit 1
  fi
done
if [ -n "$(docker ps -a -q --filter "label=com.docker.compose.project=$PROJECT")" ]; then
  echo "restore: target Compose project already has containers; refusing overwrite" >&2
  exit 1
fi

CREATED=""
cleanup() {
  if [ "$?" -ne 0 ]; then
    for name in $CREATED; do docker volume rm "$name" >/dev/null 2>&1 || true; done
    rm -rf "$CONFIG_DIR.tmp.$$"
  fi
}
trap cleanup EXIT INT TERM

for volume in "${VOLUMES[@]}"; do
  name="$(volume_name "$volume")"
  docker volume create --label "com.docker.compose.project=$PROJECT" --label "com.docker.compose.volume=$volume" "$name" >/dev/null
  CREATED="$CREATED $name"
  echo "restore: loading $volume"
  docker run --rm -i --mount "type=volume,src=$name,dst=/target" alpine:3.20 \
    tar -C /target -xf - <"$BUNDLE/volumes/$volume.tar"
done

CONFIG_TMP="$CONFIG_DIR.tmp.$$"
[ ! -e "$CONFIG_TMP" ] || { echo "restore: temporary config path already exists; refusing overwrite" >&2; exit 1; }
mkdir -m 700 -p "$CONFIG_TMP/private"
for file in "$BUNDLE"/config/.env.*; do cp "$file" "$CONFIG_TMP/$(basename "$file")"; done
cp "$BUNDLE/config/vault.hcl" "$CONFIG_TMP/vault.hcl"
for file in "$BUNDLE"/config/private/*; do cp "$file" "$CONFIG_TMP/private/$(basename "$file")"; done
chmod 600 "$CONFIG_TMP"/.env.* "$CONFIG_TMP/private"/*
chmod 644 "$CONFIG_TMP/vault.hcl"
[ ! -e "$CONFIG_DIR" ] || rmdir "$CONFIG_DIR"
mv "$CONFIG_TMP" "$CONFIG_DIR"
trap - EXIT INT TERM
echo "restore: complete; volumes and config restored, project remains stopped"
