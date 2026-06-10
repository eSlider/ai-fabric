#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -f "${ROOT_DIR}/.env" ]]; then
  cp "${ROOT_DIR}/.env.example" "${ROOT_DIR}/.env"
  echo "Created .env from .env.example"
fi

mkdir -p \
  "${ROOT_DIR}/var/gitea" \
  "${ROOT_DIR}/var/runner-1" \
  "${ROOT_DIR}/var/runner-2"

compose_plugin="${ROOT_DIR}/.docker/cli-plugins/docker-compose"
if ! docker compose version >/dev/null 2>&1; then
  if [[ ! -x "${compose_plugin}" ]]; then
    mkdir -p "${ROOT_DIR}/.docker/cli-plugins"
    arch="$(uname -m)"
    case "${arch}" in
      x86_64) compose_arch=x86_64 ;;
      aarch64|arm64) compose_arch=aarch64 ;;
      *)
        echo "unsupported architecture for docker compose: ${arch}" >&2
        exit 1
        ;;
    esac
    fetch() {
      if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" -o "$2"
      elif command -v wget >/dev/null 2>&1; then
        wget -qO "$2" "$1"
      else
        echo "curl or wget is required to install the docker compose plugin" >&2
        return 1
      fi
    }
    fetch "https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-${compose_arch}" \
      "${compose_plugin}"
    chmod +x "${compose_plugin}"
    echo "Installed docker compose plugin to ${compose_plugin}"
  fi
fi

echo "Bootstrap complete."
