#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

# Keep formatting checks dependency-free (grep fallback when rg is absent).
trailing_ws_found=0
if command -v rg >/dev/null 2>&1; then
  if rg -n "[ \t]+$" . \
    --glob "!var/**" \
    --glob "!go/**" \
    --glob "!.git/**" \
    --glob "!.cache/**" \
    --glob "!.cursor/**"; then
    trailing_ws_found=1
  fi
else
  while IFS= read -r -d '' file; do
    if grep -n '[[:space:]]$' "${file}"; then
      trailing_ws_found=1
    fi
  done < <(find . \
    -path './var' -prune -o \
    -path './go' -prune -o \
    -path './.git' -prune -o \
    -path './.cache' -prune -o \
    -path './.cursor' -prune -o \
    -type f -print0)
fi
if (( trailing_ws_found )); then
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
