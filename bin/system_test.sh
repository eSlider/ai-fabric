#!/usr/bin/env bash
# System tests: full use-case flows against the real local Gitea instance.
# Loads .env for tokens and runs the `system`-tagged Go tests.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

go test -tags system -v -count=1 ./pkg/... ./cmd/...
