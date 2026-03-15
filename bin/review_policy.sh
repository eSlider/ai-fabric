#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

errors=0

required_paths=(
  "docs/README.md"
  "docs/architecture/ai-fabric-poc.md"
  "docs/workflows/ci-cd.md"
  "docs/workflows/pr-best-practices.md"
  "docs/skills/agent-guidelines.md"
  "bin/up.sh"
  "bin/down.sh"
  "bin/pr_policy.sh"
  ".gitea/PULL_REQUEST_TEMPLATE.md"
  "CODEOWNERS"
)

for p in "${required_paths[@]}"; do
  if [[ ! -e "${p}" ]]; then
    echo "::error file=${p}::Missing required project file"
    errors=$((errors + 1))
  fi
done

if [[ -f ".env" ]]; then
  echo "::warning file=.env::.env exists locally; ensure it is never committed"
fi

if (( errors > 0 )); then
  echo "review policy check failed."
  exit 1
fi

echo "review policy check passed."
