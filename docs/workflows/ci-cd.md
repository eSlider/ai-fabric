# CI/CD Workflow (PoC)

## Baseline Gates

All push/PR events must pass:

1. `bin/fmt.sh`
2. `bin/lint.sh`
3. `bin/test.sh`
4. `bin/review_policy.sh`

## PR Review Model

- Automation provides deterministic policy checks.
- Human reviewer keeps final approval authority.
- Policy check script: `bin/review_policy.sh`.
- PR structure enforcement script: `bin/pr_policy.sh`.
- PR template: `.gitea/PULL_REQUEST_TEMPLATE.md`.
- Ownership policy: `CODEOWNERS`.

## TDD Rule

- New behavior starts with tests.
- PR is merge-blocked when tests fail.

## Additional Policy

- Follow `docs/workflows/pr-best-practices.md` for branch and review norms.
- Issue automation flow: `docs/workflows/issue-handler.md`.

## Release on main

- On push to `main` (after backpressure passes), CI calculates the next semantic version from commit messages since the latest `vX.Y.Z` tag.
- Bump policy:
  - `major`: commit contains `BREAKING CHANGE:` footer or Conventional Commit bang marker (`type!:`).
  - `minor`: at least one `feat:` commit and no major marker.
  - `patch`: any other commit.
- CI creates and pushes the new `vX.Y.Z` tag (idempotent if the tag already exists).
- CI builds release artifact `dist/ai-fabric-vX.Y.Z.tar.gz` from `HEAD` and uploads it as workflow artifact.
