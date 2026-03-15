#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -f "${ROOT_DIR}/.env" ]]; then
  cp "${ROOT_DIR}/.env.example" "${ROOT_DIR}/.env"
  echo "Created .env from .env.example"
fi

mkdir -p \
  "${ROOT_DIR}/var/gitea" \
  "${ROOT_DIR}/var/runner" \
  "${ROOT_DIR}/var/postgres"

echo "Bootstrap complete."
