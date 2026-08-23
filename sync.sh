#!/usr/bin/env bash
#
# sync.sh — single point of update for the skills catalog.
#
# Installs/updates every skill under ./skills/ into a target opencode skills
# directory (~/.config/opencode/skill/ by default). Each skill subdir is copied
# to $TARGET/<name>/; contents are mirrored so deletions in the catalog are
# propagated to the target (rsync -a --delete).
#
# Idempotent: safe to run repeatedly; only changed files are written.
#
# Usage:
#   ./sync.sh                 install skills into default target
#   ./sync.sh --dry-run       print what would change, write nothing
#   ./sync.sh --target DIR    install into DIR instead of the default
#   ./sync.sh --help          show this help
#
# Logs one line per changed skill to stderr. Exits 0 on success.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="$ROOT/skills"
DEFAULT_TARGET="$HOME/.config/opencode/skill"
TARGET="$DEFAULT_TARGET"
DRY_RUN=0

usage() {
  sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    --target) shift; TARGET="$1" ;;
    --help|-h) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

if [[ ! -d "$SRC" ]]; then
  echo "error: no skills catalog at $SRC" >&2
  exit 1
fi

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "dry-run: source=$SRC target=$TARGET"
else
  echo "syncing: source=$SRC target=$TARGET"
  mkdir -p "$TARGET"
fi

changed=0
for skill_dir in "$SRC"/*/; do
  name="$(basename "$skill_dir")"
  [[ -f "$skill_dir/SKILL.md" ]] || { echo "  skip $name (no SKILL.md)"; continue; }
  dest="$TARGET/$name"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    if [[ -d "$dest" ]]; then
      # report skills whose contents differ
      if rsync -a --delete --dry-run "$skill_dir" "$dest" | grep -q '^'; then
        echo "  would update: $name"
        ((changed++)) || true
      fi
    else
      echo "  would install: $name"
      ((changed++)) || true
    fi
    continue
  fi

  mkdir -p "$dest"
  rsync -a --delete "$skill_dir" "$dest"
  echo "  synced: $name"
  ((changed++)) || true
done

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "dry-run complete: $changed skill(s) would change"
else
  echo "done: $changed skill(s) synced into $TARGET"
fi
