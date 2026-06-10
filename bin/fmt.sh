#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

# Keep formatting checks dependency-free.
if rg -n "[ \t]+$" . \
  --glob "!var/**" \
  --glob "!.git/**" \
  --glob "!.cache/**" \
  --glob "!.cursor/**"; then
  echo "Formatting error: trailing whitespace detected."
  exit 1
fi

if command -v gofmt >/dev/null 2>&1; then
  unformatted="$(gofmt -l cmd pkg internal 2>/dev/null || true)"
  if [[ -n "${unformatted}" ]]; then
    echo "Formatting error: gofmt needed for:"
    echo "${unformatted}"
    exit 1
  fi
fi

echo "fmt check passed."
