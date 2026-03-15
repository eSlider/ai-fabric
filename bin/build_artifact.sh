#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"

usage() {
  echo "Usage: $0 --version <semver>"
}

version=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:-}"
      shift 2
      ;;
    *)
      usage
      exit 1
      ;;
  esac
done

if [[ -z "${version}" ]]; then
  usage
  exit 1
fi

if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Invalid semantic version: ${version}"
  exit 1
fi

mkdir -p "${DIST_DIR}"
artifact_name="ai-fabric-v${version}.tar.gz"
artifact_path="${DIST_DIR}/${artifact_name}"

git -C "${ROOT_DIR}" archive --format=tar.gz \
  --prefix="ai-fabric-v${version}/" \
  -o "${artifact_path}" \
  HEAD

echo "${artifact_path}"
