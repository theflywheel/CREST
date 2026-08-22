#!/usr/bin/env bash
#
# P0 spike #2 — publish → inclusion proof → version resolve, against a live DeDi node.
#
# This is the "short script" issue #2 asks for. It is a spike, not a test: it
# proves the registry substrate can do what Blueprint §3 assumes, and every
# place it could not is a finding for the memo (#4), not a bug to fix here.
#
# It uses DeDi's own `dedid` CLI to sign write requests. CREST deliberately does
# not re-implement DeDi's publisher signature — a second implementation of
# somebody else's authentication scheme is a liability, not an integration.
#
# Prerequisites (see docs/spikes/dedi.md):
#   DEDI_URL          base URL of a running node          (default http://localhost:58081)
#   DEDI_CLI          path to the dedid binary            (default: dedid on PATH)
#   DEDI_KEY_FILE     publisher private key               (default ./keys/publisher.key)
#   DEDI_KID          publisher key id                    (default crest-spike)
#   DEDI_NAMESPACE    namespace the key may write to      (default crest.local)

set -euo pipefail

DEDI_URL="${DEDI_URL:-http://localhost:58081}"
DEDI_CLI="${DEDI_CLI:-dedid}"
DEDI_KEY_FILE="${DEDI_KEY_FILE:-./keys/publisher.key}"
DEDI_KID="${DEDI_KID:-crest-spike}"
NS="${DEDI_NAMESPACE:-crest.local}"
REG="work-definitions"
REC="WD-4471"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

step()  { printf '\n\033[1m── %s\033[0m\n' "$*"; }
fail()  { printf '\033[31mFAIL: %s\033[0m\n' "$*" >&2; exit 1; }
ok()    { printf '\033[32m  ok\033[0m %s\n' "$*"; }

# DeDi's write API takes {"payload": {...}}; the signature covers the whole body,
# so the file that is signed must be the file that is sent, byte for byte.
send() {
  local method="$1" path="$2" body="$3" precond="$4"
  local hdrs
  hdrs="$("$DEDI_CLI" sign -key "$DEDI_KEY_FILE" -kid "$DEDI_KID" \
            -method "$method" -path "$path" -body "$body" "$precond" -curl)"
  local code
  code="$(eval curl -sS -o "$work/resp.json" -w '%{http_code}' \
            -X "$method" "$hdrs" -H "'Content-Type: application/json'" \
            --data-binary "@$body" "$DEDI_URL$path")"
  echo "$code"
}

expect_2xx() {
  local code="$1" what="$2"
  case "$code" in
    2*) ok "$what ($code)" ;;
    *)  cat "$work/resp.json" >&2; fail "$what returned $code" ;;
  esac
}

step "0 · node is reachable"
curl -sS --fail "$DEDI_URL/healthz" > "$work/health.json" || fail "no node at $DEDI_URL"
python3 -c "import json,sys; d=json.load(open('$work/health.json')); print('  node:', d)"

step "1 · publish the namespace and the work-definition registry"
cat > "$work/ns.json" <<JSON
{"payload":{"name":"CREST","description":"CREST work definitions — P0 spike"}}
JSON
expect_2xx "$(send PUT "/admin/namespaces/$NS" "$work/ns.json" -create)" "namespace $NS"

cat > "$work/reg.json" <<JSON
{"payload":{"name":"Work definitions","description":"Blueprint §3 — the public face of a work definition"}}
JSON
expect_2xx "$(send PUT "/admin/namespaces/$NS/registries/$REG" "$work/reg.json" -create)" "registry $REG"

step "2 · publish v1 of a work-definition record"
# Public face only. A work definition is public by design (Blueprint §3): the
# worker-facing and verifier-facing faces carry no personal data, so nothing
# here is subject to W9. If a future field would be, it does not belong on DeDi.
cat > "$work/v1.json" <<JSON
{"payload":{
  "definitionId": "$REC",
  "version": "1.0",
  "title": "Insecticide-treated bednet distributed",
  "unit": "bednet",
  "status": "ACTIVE",
  "publicFace": {
    "whatCounts": "One bednet handed to a household, recorded against that household",
    "evidenceExpected": "Distribution roster line, or supervisor confirmation"
  }
}}
JSON
expect_2xx "$(send POST "/admin/namespaces/$NS/registries/$REG/records/$REC/publish" "$work/v1.json" -create)" "record $REC v1"

step "3 · fetch it back and validate the proof INDEPENDENTLY"
curl -sS --fail "$DEDI_URL/dedi/lookup/$NS/$REG/$REC?proof=inclusion" > "$work/proof1.json" \
  || fail "lookup with proof=inclusion failed"

# Asking DeDi to check its own proof would only establish that DeDi agrees with
# itself. dediproof is a second implementation written from the wire format.
#
# In production the verifier key arrives out of band — that is what makes it a
# trust root. For a spike against a node that minted its own key we derive it
# from the node's published manifest, and then CHECK that the key the manifest
# advertises is the key that actually signed the checkpoint. That check is not
# ceremony: the manifest is regenerated on a schedule, so just after a key
# change it still advertises the previous key, and a key you cannot cross-check
# is a key you should not trust.
if [ -z "${DEDI_VERIFIER_KEY:-}" ]; then
  DEDI_VERIFIER_KEY="$(./tools/spikes/dedi-verifier-key.py "$DEDI_URL")" \
    || fail "could not derive the verifier key"
  echo "  derived verifier key: ${DEDI_VERIFIER_KEY%%+*}+…"
fi
go run ./tools/spikes/dediproof -key "$DEDI_VERIFIER_KEY" < "$work/proof1.json" \
  || fail "inclusion proof did not validate"

step "4 · publish v2 and confirm BOTH versions still resolve"
# Immutability is the property under test. A definition that can be edited in
# place makes every credential ever issued against it unverifiable — the record
# a verifier resolves must be the record the credential was issued against.
# FINDING (#4): the If-Match precondition wants "<digest>-<state>", but the
# lookup response returns those as two separate fields and no combined tag.
# Callers must reassemble it, which means every client re-derives a format only
# the server should own. Minor, but it is exactly the kind of thing that drifts.
tag="$(python3 -c "
import json,sys
d=json.load(open('$work/proof1.json'))['data']
print(f\"{d['digest']}-{d['state']}\")")"
[ -n "$tag" ] || fail "could not build a version tag from the v1 response"
echo "  version tag: $tag"

cat > "$work/v2.json" <<JSON
{"payload":{
  "definitionId": "$REC",
  "version": "1.1",
  "title": "Insecticide-treated bednet distributed",
  "unit": "bednet",
  "status": "ACTIVE",
  "publicFace": {
    "whatCounts": "One bednet handed to a household, recorded against that household",
    "evidenceExpected": "Distribution roster line, or supervisor confirmation",
    "changedInThisVersion": "Roster line is now the primary evidence; supervisor confirmation is the fallback"
  }
}}
JSON
expect_2xx "$(send POST "/admin/namespaces/$NS/registries/$REG/records/$REC/publish" "$work/v2.json" "-if-match=$tag")" "record $REC v2"

curl -sS --fail "$DEDI_URL/dedi/versions/$NS/$REG/$REC" > "$work/versions.json" || fail "version listing failed"
python3 - "$work/versions.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
def find(o):
    if isinstance(o, dict):
        for k, v in o.items():
            if k in ("versions", "results") and isinstance(v, list):
                return v
            r = find(v)
            if r is not None:
                return r
    return None
vs = find(d) or []
print(f"  {len(vs)} version(s) listed")
if len(vs) < 2:
    print("  FINDING: v1 did not survive publishing v2", file=sys.stderr); sys.exit(1)
PY

# The property that actually matters is not that v1 is *listed* but that the
# proof issued against v1 still validates. A credential issued under v1 has to
# stay verifiable after the definition moves on, or W6 breaks quietly.
ver1="$(python3 -c "
import json
d = json.load(open('$work/proof1.json'))['data']
print(d['version'])")"
curl -sS --fail "$DEDI_URL/dedi/lookup/$NS/$REG/$REC?version_id=$ver1&proof=inclusion" > "$work/proof1again.json" \
  || fail "v1 no longer resolves by version reference"

# Assert the pin actually pinned. An unknown query parameter is ignored rather
# than rejected (FINDING, #4): a client that misspells version_id silently gets
# the LATEST version, with a perfectly valid proof attached. In CREST that means
# a verifier resolving the wrong definition and having no way to notice.
python3 - "$work/proof1again.json" <<'PY'
import json, sys
p = json.load(open(sys.argv[1]))["proof"]["leaf"]
if p["version_num"] != 1:
    print(f"  pinned lookup returned version_num={p['version_num']}, want 1", file=sys.stderr)
    sys.exit(1)
PY
go run ./tools/spikes/dediproof -key "$DEDI_VERIFIER_KEY" < "$work/proof1again.json" \
  || fail "FINDING: the v1 inclusion proof stopped validating once v2 was published"
ok "both versions resolve, v1 pins correctly, and v1's proof still validates"

printf '\n\033[32mspike passed\033[0m — record anything surprising in the findings memo (#4)\n'
