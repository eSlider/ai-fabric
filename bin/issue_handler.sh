#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

set -a
source "${ROOT_DIR}/.env"
set +a

export ISSUE_HANDLER_TRIGGER_SCRIPT="${ISSUE_HANDLER_TRIGGER_SCRIPT:-${ROOT_DIR}/bin/issue_handler.sh}"

exec ./bin/issue_handler.py "$@"
