#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

scripts=(bin/*.sh)
for script in "${scripts[@]}"; do
  bash -n "${script}"
done

if [[ -f "${ROOT_DIR}/go.mod" ]] && command -v go >/dev/null 2>&1; then
  (
    cd "${ROOT_DIR}"
    go build ./cmd/... ./pkg/... ./internal/...
    go vet ./cmd/... ./pkg/... ./internal/...
  )
fi

echo "lint check passed."
