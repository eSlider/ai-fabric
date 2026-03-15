#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

required=(
  "${ROOT_DIR}/README.md"
  "${ROOT_DIR}/docker-compose.yml"
  "${ROOT_DIR}/docs/README.md"
  "${ROOT_DIR}/.gitea/workflows/ci.yml"
)

for path in "${required[@]}"; do
  [[ -f "${path}" ]] || { echo "Missing required file: ${path}"; exit 1; }
done

echo "test check passed."
