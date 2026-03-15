#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

# Keep formatting checks dependency-free.
if rg -n "[ \t]+$" . \
  --glob "!var/**" \
  --glob "!.git/**" \
  --glob "!.cursor/**"; then
  echo "Formatting error: trailing whitespace detected."
  exit 1
fi

echo "fmt check passed."
