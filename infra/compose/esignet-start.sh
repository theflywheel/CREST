#!/bin/bash
# Start eSignet without the upstream image's HSM-client installer.
#
# The stock entrypoint (configure_start.sh) skips that installer only when
# active_profile_env is *exactly* the string "local". Every published quickstart
# sets "default,local", so the skip never fires and the installer runs — it ends
# in `sudo ./install.sh`, which needs a terminal, and on restart `mv` collides
# with the directory the previous attempt left behind. The container then loops.
#
# We cannot use profile "local" alone to dodge it: application-local.properties
# is a 55-line overlay on a 460-line application-default.properties, so "local"
# by itself is missing most of eSignet's configuration. The profile we need is
# the one that triggers the bug, so the entrypoint is what has to go.
#
# CREST has no HSM. Keys live in the database under the local profile's
# file-based keystore, which is the right shape for a spike and the wrong shape
# for production — see docs/p0-findings.md before this reaches a pilot.
set -euo pipefail

# The one useful thing the stock entrypoint did: put the plugin jars where
# Spring Boot's PropertiesLauncher will find them.
if [[ -n "${plugin_name_env:-}" ]]; then
  IFS=',' read -ra plugins <<<"$plugin_name_env"
  for plugin in "${plugins[@]}"; do
    plugin="$(echo "$plugin" | xargs)"
    [[ -z "$plugin" ]] && continue
    if [[ ! -f "${plugins_path_env}/${plugin}" ]]; then
      echo "plugin '${plugin}' not found in ${plugins_path_env}" >&2
      exit 1
    fi
    cp -f "${plugins_path_env}/${plugin}" "${loader_path_env}/"
    echo "loaded plugin ${plugin}"
  done
fi

cd "${work_dir}"
exec java \
  -jar \
  -Dloader.path="${loader_path_env}" \
  -Dspring.profiles.active="${active_profile_env}" \
  "${JAR_NAME:-esignet-service.jar}"
