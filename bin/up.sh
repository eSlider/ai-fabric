#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"${ROOT_DIR}/bin/bootstrap.sh"
docker compose -f "${ROOT_DIR}/docker-compose.yml" up -d

echo "Stack started."
echo "Gitea URL: http://localhost:${GITEA_HTTP_PORT:-3000}"
