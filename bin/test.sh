#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

required=(
  "${ROOT_DIR}/README.md"
  "${ROOT_DIR}/docker-compose.yml"
  "${ROOT_DIR}/docs/README.md"
  "${ROOT_DIR}/docs/workflows/pr-best-practices.md"
  "${ROOT_DIR}/docs/workflows/issue-handler.md"
  "${ROOT_DIR}/docs/skills/solution-architect.md"
  "${ROOT_DIR}/docs/skills/developer.md"
  "${ROOT_DIR}/.gitea/workflows/ci.yml"
  "${ROOT_DIR}/.gitea/PULL_REQUEST_TEMPLATE.md"
  "${ROOT_DIR}/CODEOWNERS"
  "${ROOT_DIR}/bin/pr_policy.sh"
)

for path in "${required[@]}"; do
  [[ -f "${path}" ]] || { echo "Missing required file: ${path}"; exit 1; }
done

# Keep Go package discovery scoped to source directories.
# `var/` contains runtime service data and must stay out of module/package scans.
if [[ -f "${ROOT_DIR}/go.mod" ]] && command -v go >/dev/null 2>&1; then
  (
    cd "${ROOT_DIR}"
    go test ./cmd/... ./pkg/...
  )
fi

echo "test check passed."
