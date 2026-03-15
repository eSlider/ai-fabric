# PR Best Practices (Workflow-Enforced)

## Branching

- Use short-lived branches: `feature/*`, `fix/*`, `chore/*`.
- Keep PRs single-purpose and small enough for fast review.

## Required PR Structure

PR description must include:

- `## Problem`
- `## Solution`
- `## Risks`
- `## Test Plan`
- `## Rollback`
- `## Issue Link` with `Closes|Fixes|Refs #<id>`
- `## AI Notes`

Enforced by: `bin/pr_policy.sh` in CI on pull requests.

## Review and Merge

- Automation (`fmt`, `lint`, `test`, policy checks) must be green.
- Human review remains final merge gate.
- Prefer squash merge for clean history.

## Ownership and Critical Paths

- `CODEOWNERS` defines required owner awareness for sensitive files.
- Critical paths include workflows, infra compose, scripts, and workflow docs.
