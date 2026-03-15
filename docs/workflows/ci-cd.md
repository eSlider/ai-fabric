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

## TDD Rule

- New behavior starts with tests.
- PR is merge-blocked when tests fail.
