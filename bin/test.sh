#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

required=(
  "${ROOT_DIR}/README.md"
  "${ROOT_DIR}/docker-compose.yml"
  "${ROOT_DIR}/docs/README.md"
  "${ROOT_DIR}/docs/architecture/structure.md"
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

# CI jobs use runs-on: ubuntu-latest; the Gitea runner must advertise the same label.
runner_script="${ROOT_DIR}/bin/gitea-runner-command.sh"
expected_label='ubuntu-latest:docker://gitea/runner-images:ubuntu-latest'
default_assignment='GITEA_RUNNER_LABELS="${GITEA_RUNNER_LABELS:-'"${expected_label}"'}"'
if [[ "$(grep -cF -- "${default_assignment}" "${runner_script}")" -ne 1 ]]; then
  echo "gitea-runner-command.sh must define GITEA_RUNNER_LABELS default exactly once"
  exit 1
fi
if [[ "$(grep -cF -- '${GITEA_RUNNER_LABELS}' "${runner_script}")" -lt 2 ]]; then
  echo "gitea-runner-command.sh must reference \${GITEA_RUNNER_LABELS} in config patch and register --labels"
  exit 1
fi
if [[ "$(grep -cF -- "${expected_label}" "${runner_script}")" -gt 1 ]]; then
  echo "gitea-runner-command.sh must not duplicate the literal runner label string"
  exit 1
fi

# Keep Go package discovery scoped to source directories.
# `var/` contains runtime service data and must stay out of module/package scans.
if [[ -f "${ROOT_DIR}/go.mod" ]] && command -v go >/dev/null 2>&1; then
  (
    cd "${ROOT_DIR}"
    go test ./cmd/... ./pkg/...
  )
fi

echo "test check passed."
