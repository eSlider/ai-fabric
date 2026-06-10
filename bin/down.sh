#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"${ROOT_DIR}/bin/compose.sh" -f "${ROOT_DIR}/docker-compose.yml" down
echo "Stack stopped."
