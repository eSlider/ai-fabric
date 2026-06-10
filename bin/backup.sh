#!/usr/bin/env bash
# Backup Gitea data (SQLite DB, repositories, actions state) into a tarball.
# Usage: ./bin/backup.sh [output-dir]   (default: var/backups)
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-${ROOT_DIR}/var/backups}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
ARCHIVE="${OUT_DIR}/gitea-${STAMP}.tar.gz"
KEEP=7

mkdir -p "${OUT_DIR}"

# A consistent SQLite snapshot requires Gitea to be paused.
echo "Stopping gitea for a consistent snapshot..."
docker compose --project-directory "${ROOT_DIR}" stop gitea

cleanup() {
  echo "Starting gitea back up..."
  docker compose --project-directory "${ROOT_DIR}" start gitea
}
trap cleanup EXIT

tar -czf "${ARCHIVE}" -C "${ROOT_DIR}/var" gitea
echo "Backup written: ${ARCHIVE} ($(du -h "${ARCHIVE}" | cut -f1))"

# Rotate: keep the most recent ${KEEP} backups.
ls -1t "${OUT_DIR}"/gitea-*.tar.gz 2>/dev/null | tail -n "+$((KEEP + 1))" | xargs -r rm -f
