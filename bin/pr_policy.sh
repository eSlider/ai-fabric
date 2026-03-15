#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if [[ "${GITHUB_EVENT_NAME:-}" != "pull_request" ]]; then
  echo "pr policy skipped: event is '${GITHUB_EVENT_NAME:-unknown}'"
  exit 0
fi

if [[ -z "${GITHUB_EVENT_PATH:-}" || ! -f "${GITHUB_EVENT_PATH}" ]]; then
  echo "::error::GITHUB_EVENT_PATH is missing for pull_request event"
  exit 1
fi

python3 - <<'PY'
import json
import os
import re
import sys

event_path = os.environ["GITHUB_EVENT_PATH"]
with open(event_path, "r", encoding="utf-8") as f:
    payload = json.load(f)

pr = payload.get("pull_request") or {}
body = pr.get("body") or ""

required_sections = [
    "## Problem",
    "## Solution",
    "## Risks",
    "## Test Plan",
    "## Rollback",
    "## Issue Link",
    "## AI Notes",
]

missing = [section for section in required_sections if section not in body]
if missing:
    for section in missing:
        print(f"::error::PR template section missing: {section}")
    sys.exit(1)

issue_ref = re.search(r"(?im)\\b(closes|fixes|refs)\\s+#\\d+\\b", body)
if not issue_ref:
    print("::error::Issue Link must include Closes/Fixes/Refs #<id>")
    sys.exit(1)

print("pr policy check passed.")
PY
