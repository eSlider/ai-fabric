#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

scripts=(bin/*.sh)
for script in "${scripts[@]}"; do
  bash -n "${script}"
done

python3 -m py_compile bin/*.py

if command -v ruff >/dev/null 2>&1; then
  ruff check bin/*.py
elif [[ "${CI:-}" == "true" ]]; then
  echo "ruff is required in CI but not installed"
  exit 1
else
  echo "warning: ruff is not installed locally; skipping ruff check"
fi

echo "lint check passed."
