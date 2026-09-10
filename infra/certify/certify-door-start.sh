#!/bin/sh
# The Certify door's entrypoint (#203). Writes nginx's resolver from the
# container's own resolv.conf and its upstream from CERTIFY_UPSTREAM, then
# starts nginx — the same shape as infra/esignet-ui/esignet-ui-start.sh.
set -e
NS=$(awk '/^nameserver/ { print $2; exit }' /etc/resolv.conf)
if [ -z "$NS" ]; then
  echo "no nameserver in /etc/resolv.conf; nginx cannot re-resolve upstreams" >&2
  exit 1
fi
case "$NS" in *:*) NS="[$NS]" ;; esac
echo "resolver $NS valid=10s ipv6=on;" > /tmp/resolver.conf
UP="${CERTIFY_UPSTREAM:-http://crest-certify.railway.internal:8090}"
echo "map \$host \$certify { default \"$UP\"; }" > /tmp/upstream.conf
mkdir -p /tmp/nginx-client
echo "certify door: resolver $NS, upstream $UP"
exec nginx -g 'daemon off;'
