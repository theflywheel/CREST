#!/usr/bin/env bash
set -euo pipefail

# Local infrastructure authentication only. The generated file contains no
# business data and is ignored by Git and Docker; production credentials still
# come from the deployment environment.
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
auth_file=${CREST_LOCAL_AUTH_FILE:-"$repo_root/.env.crest-local-auth"}

die() {
	printf '%s\n' "$1" >&2
	exit 1
}

read_token() {
	[[ -r "$auth_file" ]] || die "local service-auth file is unavailable"
	local token
	token=$(awk -F= '$1 == "CREST_SERVICE_TOKEN" { print substr($0, index($0, "=") + 1); exit }' "$auth_file")
	valid_token "$token" || die "local service-auth file contains an invalid token"
	printf '%s' "$token"
}

valid_token() {
	local token=$1
	[[ ${#token} -ge 32 && "$token" != *$'\n'* && "$token" != *$'\r'* ]]
}

ensure_file() {
	if [[ -n "${CREST_SERVICE_TOKEN:-}" ]]; then
		valid_token "$CREST_SERVICE_TOKEN" || die "CREST_SERVICE_TOKEN must contain at least 32 bytes and no newlines"
		return
	fi
	if [[ -n "${CREST_SERVICE_PRIVATE_KEY:-}" || -n "${CREST_SERVICE_PEERS_JSON:-}" ]]; then
		return
	fi
	if [[ -s "$auth_file" ]]; then
		read_token >/dev/null
		return
	fi
	mkdir -p "$(dirname "$auth_file")"
	local lock="${auth_file}.lock"
	if mkdir "$lock" 2>/dev/null; then
		trap 'rmdir "$lock" 2>/dev/null || true' EXIT
		if [[ ! -s "$auth_file" ]]; then
			local token tmp
			token=$(openssl rand -hex 32 2>/dev/null || python3 -c 'import secrets; print(secrets.token_hex(32))')
			tmp=$(mktemp "${auth_file}.tmp.XXXXXX")
			chmod 600 "$tmp"
			printf 'CREST_SERVICE_TOKEN=%s\n' "$token" >"$tmp"
			mv "$tmp" "$auth_file"
		fi
		rmdir "$lock" 2>/dev/null || true
		trap - EXIT
	else
		for _ in $(seq 1 100); do
			[[ -s "$auth_file" ]] && { read_token >/dev/null; return; }
			sleep 0.05
		done
		die "timed out waiting for local service-auth file"
	fi
}

command=${1:-}
shift || true
case "$command" in
compose)
	ensure_file
	if [[ -z "${CREST_SERVICE_TOKEN:-}" && -z "${CREST_SERVICE_PRIVATE_KEY:-}" && -z "${CREST_SERVICE_PEERS_JSON:-}" ]]; then
		export CREST_SERVICE_TOKEN=$(read_token)
	fi
	set -- -f "$repo_root/infra/compose/docker-compose.yml" "$@"
	exec docker compose "$@"
	;;
run)
	ensure_file
	if [[ -z "${CREST_SERVICE_TOKEN:-}" && -z "${CREST_SERVICE_PRIVATE_KEY:-}" && -z "${CREST_SERVICE_PEERS_JSON:-}" ]]; then
		export CREST_SERVICE_TOKEN=$(read_token)
	fi
	[[ "${1:-}" == "--" ]] && shift
	[[ $# -gt 0 ]] || die "local-service-auth run requires a command"
	exec "$@"
	;;
*)
	die "usage: local-service-auth.sh {compose|run} ..."
	;;
esac
