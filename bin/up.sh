#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"${ROOT_DIR}/bin/bootstrap.sh"
extra_args=()
if [[ "${1:-}" == "--no-build" ]]; then
  extra_args+=(--no-build)
fi
"${ROOT_DIR}/bin/compose.sh" -f "${ROOT_DIR}/docker-compose.yml" up -d "${extra_args[@]}"

echo "Stack started."
echo "Gitea URL: http://localhost:${GITEA_HTTP_PORT:-3000}"
