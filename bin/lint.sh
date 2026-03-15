#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

scripts=(bin/*.sh)
for script in "${scripts[@]}"; do
  bash -n "${script}"
done

echo "lint check passed."
