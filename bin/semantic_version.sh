#!/usr/bin/env bash
set -euo pipefail

SEMVER_TAG_REGEX='^v([0-9]+)\.([0-9]+)\.([0-9]+)$'

latest_semver_tag() {
  local latest=""
  while IFS= read -r tag; do
    [[ "${tag}" =~ ${SEMVER_TAG_REGEX} ]] || continue
    if [[ -z "${latest}" ]]; then
      latest="${tag}"
      continue
    fi
    if [[ "$(printf '%s\n%s\n' "${latest#v}" "${tag#v}" | sort -V | tail -n1)" == "${tag#v}" ]]; then
      latest="${tag}"
    fi
  done < <(git tag --list)
  echo "${latest}"
}

required_bump() {
  local commits="$1"
  local has_minor=0
  local has_patch=0

  while IFS= read -r message; do
    [[ -n "${message}" ]] || continue
    local first_line="${message%%$'\n'*}"
    if [[ "${message}" == *"BREAKING CHANGE:"* ]] || printf '%s\n' "${first_line}" | rg -q '^[A-Za-z]+(\([^)]+\))?!:'; then
      echo "major"
      return
    fi
    if printf '%s\n' "${first_line}" | rg -q '^feat(\([^)]+\))?:'; then
      has_minor=1
      continue
    fi
    has_patch=1
  done <<< "${commits}"

  if [[ "${has_minor}" -eq 1 ]]; then
    echo "minor"
    return
  fi
  if [[ "${has_patch}" -eq 1 ]]; then
    echo "patch"
    return
  fi
  echo "patch"
}

bump_version() {
  local version="$1"
  local bump="$2"
  local major minor patch
  IFS='.' read -r major minor patch <<< "${version}"
  case "${bump}" in
    major) echo "$((major + 1)).0.0" ;;
    minor) echo "${major}.$((minor + 1)).0" ;;
    patch) echo "${major}.${minor}.$((patch + 1))" ;;
    *) echo "invalid bump level: ${bump}" >&2; exit 1 ;;
  esac
}

resolve_next_version() {
  local head_semver_tag
  head_semver_tag="$(git tag --points-at HEAD | awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/ { print }' | sort -V | tail -n1 || true)"
  if [[ -n "${head_semver_tag}" ]]; then
    echo "${head_semver_tag#v}"
    return
  fi

  local latest_tag
  latest_tag="$(latest_semver_tag)"

  local commits_output
  if [[ -z "${latest_tag}" ]]; then
    commits_output="$(git log --format=%B)"
  else
    commits_output="$(git log --format=%B "${latest_tag}..HEAD")"
  fi

  local bump
  bump="$(required_bump "${commits_output}")"
  local base_version="0.0.0"
  if [[ -n "${latest_tag}" ]]; then
    base_version="${latest_tag#v}"
  fi
  bump_version "${base_version}" "${bump}"
}

main() {
  local command="${1:-}"
  case "${command}" in
    next) resolve_next_version ;;
    *)
      echo "Usage: $0 next" >&2
      exit 1
      ;;
  esac
}

main "$@"
