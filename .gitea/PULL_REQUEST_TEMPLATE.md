## Problem
What problem does this PR solve?

## Solution
What changed and why is this approach chosen?

## Risks
What can break? Include security/compatibility concerns.

## Test Plan
- [ ] Added or updated tests first (TDD where applicable)
- [ ] `./bin/fmt.sh`
- [ ] `./bin/lint.sh`
- [ ] `./bin/test.sh`
- [ ] Manual checks performed (describe)

## Reused
Existing code, libraries, or SDK types reused instead of reimplementing (engineering-principles §1):

- `pkg/gitea` — typed Gitea client / service layer
- `code.gitea.io/sdk/gitea` — official Gitea SDK types and API client
- `github.com/eslider/go-gitea-helpers` — SDK client construction helpers

## Rollback
How to safely rollback if this causes issues?

## Issue Link
Closes #<issue-id>

## AI Notes
Assumptions, unresolved questions, and limits of AI-generated changes.
