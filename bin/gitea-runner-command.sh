#!/usr/bin/env sh
set -eu

mkdir -p /tmp/runner

if [ ! -f "${CONFIG_FILE}" ]; then
  act_runner generate-config | sed 's#network: ""#network: "ai-fabric_default"#' > "${CONFIG_FILE}"
fi

if [ -z "${GITEA_RUNNER_REGISTRATION_TOKEN:-}" ] || [ "${GITEA_RUNNER_REGISTRATION_TOKEN}" = "replace-with-runner-token" ]; then
  echo "GITEA_RUNNER_REGISTRATION_TOKEN is not set. Waiting for a valid token in .env"
  exec sleep infinity
fi

if [ ! -f /tmp/runner/.runner ]; then
  act_runner register --no-interactive \
    --instance "${GITEA_INSTANCE_URL}" \
    --token "${GITEA_RUNNER_REGISTRATION_TOKEN}" \
    --name "${GITEA_RUNNER_NAME}-${HOSTNAME}"
  touch /tmp/runner/.runner
fi

exec act_runner daemon --config "${CONFIG_FILE}"
