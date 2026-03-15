#!/usr/bin/env python3
import argparse
import re
import subprocess
from typing import Iterable

SEMVER_TAG_PATTERN = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
BANG_CHANGE_PATTERN = re.compile(r"^[a-zA-Z]+(\([^)]+\))?!:")
FEAT_PATTERN = re.compile(r"^feat(\([^)]+\))?:")


def latest_semver_tag(tags: Iterable[str]) -> str | None:
    parsed: list[tuple[int, int, int, str]] = []
    for raw_tag in tags:
        tag = raw_tag.strip()
        match = SEMVER_TAG_PATTERN.match(tag)
        if not match:
            continue
        major = int(match.group(1))
        minor = int(match.group(2))
        patch = int(match.group(3))
        parsed.append((major, minor, patch, tag))
    if not parsed:
        return None
    parsed.sort(key=lambda item: (item[0], item[1], item[2]))
    return parsed[-1][3]


def required_bump(commits: Iterable[str]) -> str:
    has_minor = False
    has_patch = False
    for message in commits:
        normalized = message.strip()
        if not normalized:
            continue
        first_line = normalized.splitlines()[0]
        if "BREAKING CHANGE:" in normalized or BANG_CHANGE_PATTERN.match(first_line):
            return "major"
        if FEAT_PATTERN.match(first_line):
            has_minor = True
            continue
        has_patch = True
    if has_minor:
        return "minor"
    if has_patch:
        return "patch"
    return "patch"


def bump_version(version: str, bump: str) -> str:
    match = re.match(r"^(\d+)\.(\d+)\.(\d+)$", version)
    if not match:
        raise ValueError(f"invalid version: {version}")
    major = int(match.group(1))
    minor = int(match.group(2))
    patch = int(match.group(3))
    if bump == "major":
        return f"{major + 1}.0.0"
    if bump == "minor":
        return f"{major}.{minor + 1}.0"
    if bump == "patch":
        return f"{major}.{minor}.{patch + 1}"
    raise ValueError(f"invalid bump level: {bump}")


def compute_next_version(latest_tag: str | None, commits: Iterable[str]) -> str:
    bump = required_bump(commits)
    base_version = "0.0.0" if latest_tag is None else latest_tag.lstrip("v")
    return bump_version(base_version, bump)


def run_git(args: list[str]) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def tags_pointing_at_head() -> list[str]:
    output = run_git(["tag", "--points-at", "HEAD"])
    if not output:
        return []
    return [line for line in output.splitlines() if line]


def resolve_next_version() -> str:
    head_semver_tag = latest_semver_tag(tags_pointing_at_head())
    if head_semver_tag is not None:
        return head_semver_tag.lstrip("v")

    all_tags_output = run_git(["tag", "--list"])
    all_tags = all_tags_output.splitlines() if all_tags_output else []
    latest_tag = latest_semver_tag(all_tags)
    if latest_tag is None:
        commits_output = run_git(["log", "--format=%B"])
    else:
        commits_output = run_git(["log", "--format=%B", f"{latest_tag}..HEAD"])
    commits = [entry for entry in commits_output.split("\n\n") if entry.strip()]
    return compute_next_version(latest_tag, commits)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Resolve semantic version from git history."
    )
    parser.add_argument(
        "command",
        choices=["next"],
        help="Print next semantic version for current HEAD.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.command == "next":
        print(resolve_next_version())
        return 0
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
