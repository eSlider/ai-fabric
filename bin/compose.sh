#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -x "${ROOT_DIR}/.docker/cli-plugins/docker-compose" ]]; then
  export DOCKER_CONFIG="${ROOT_DIR}/.docker"
fi

if docker compose version >/dev/null 2>&1; then
  exec docker compose "$@"
fi

if command -v docker-compose >/dev/null 2>&1; then
  exec docker-compose "$@"
fi

echo "docker compose is not available (install the compose plugin or docker-compose)" >&2
exit 1
