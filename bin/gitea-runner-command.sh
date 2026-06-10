#!/usr/bin/env sh
set -eu

GITEA_RUNNER_LABELS="${GITEA_RUNNER_LABELS:-ubuntu-latest:docker://gitea/runner-images:ubuntu-latest}"

mkdir -p /tmp/runner
# act_runner writes its registration file (.runner) to the working directory;
# /tmp/runner is volume-mounted so registration survives container recreation.
cd /tmp/runner

if [ ! -f "${CONFIG_FILE}" ]; then
  act_runner generate-config \
    | sed 's#network: ""#network: "ai-fabric_default"#' \
    | sed "s#  labels: \[\]#  labels:\
    - \"${GITEA_RUNNER_LABELS}\"#" \
    > "${CONFIG_FILE}"
fi

if [ -z "${GITEA_RUNNER_REGISTRATION_TOKEN:-}" ] || [ "${GITEA_RUNNER_REGISTRATION_TOKEN}" = "replace-with-runner-token" ]; then
  echo "GITEA_RUNNER_REGISTRATION_TOKEN is not set. Waiting for a valid token in .env"
  exec sleep infinity
fi

# Already-registered runners keep stale labels until /tmp/runner/.runner (and
# persisted config) are removed or the container/volume is recreated.
if [ ! -f /tmp/runner/.runner ]; then
  act_runner register --no-interactive \
    --instance "${GITEA_INSTANCE_URL}" \
    --token "${GITEA_RUNNER_REGISTRATION_TOKEN}" \
    --name "${GITEA_RUNNER_NAME}-${HOSTNAME}" \
    --labels "${GITEA_RUNNER_LABELS}"
  touch /tmp/runner/.runner
fi

exec act_runner daemon --config "${CONFIG_FILE}"
