#!/bin/sh
# eSignet UI entrypoint.
#
# It exists to write nginx's resolver line from the container's own
# /etc/resolv.conf before nginx starts.
#
# nginx resolves an upstream named literally once, at configuration load, and
# then holds that address forever. On Railway a redeployed service comes back on
# a different private IPv6 address, so the proxy keeps dialling an address
# nothing answers on: every request hangs until it times out, with the API
# service healthy on its own hostname and nothing in either log naming the
# cause. Re-resolving per request needs a `resolver` directive, and the resolver
# address is assigned by the platform rather than fixed.
set -e

NS=$(awk '/^nameserver/ { print $2; exit }' /etc/resolv.conf)
if [ -z "$NS" ]; then
  echo "no nameserver in /etc/resolv.conf; nginx cannot re-resolve upstreams" >&2
  exit 1
fi

# An IPv6 literal has to be bracketed in the resolver directive.
case "$NS" in
  *:*) NS="[$NS]" ;;
esac

echo "resolver $NS valid=10s ipv6=on;" > /tmp/resolver.conf
echo "using resolver $NS"

exec sh /home/mosip/configure_start.sh "$@"
