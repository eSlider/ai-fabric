# Issue Handler Workflow

## Purpose

Automatically process each open issue: the architect analyses the task first,
the developer implements it on a branch and opens a PR, CI failures are fixed
within a bounded budget, and the result is reviewed automatically.

## Roles (separate Gitea users)

| Role | Gitea user | Token env | Responsibility |
|------|-----------|-----------|----------------|
| Architect | `ai-architect` | `GITEA_ARCHITECT_TOKEN` | Analyses new issues, appends the solution structure to the issue body, reviews PRs whose fix budget is exhausted |
| Developer | `ai-developer` | `GITEA_HANDLER_TOKEN` | Implements issues, commits/pushes (commits are authored as this user), fixes CI failures |
| Reviewer | `ai-reviewer` | `GITEA_REVIEWER_TOKEN` | Posts one automated review per PR head SHA |

All tokens fall back to `GITEA_BOT_TOKEN` when empty.

## Triggers

1. Polling: every `ISSUE_POLL_INTERVAL_SEC` (default 45s) all open issues are scanned.
2. Webhooks on `:8082` (`/webhook`, HMAC-validated with `GITEA_WEBHOOK_SECRET`):
   - `issues` (opened) — architect analysis starts immediately.
   - `pull_request` (opened/synchronized) — automated review; (closed+merged) — linked issue gets `status:completed`.
   - `status` (commit status failure from Gitea Actions) — CI fixer for the matching PR.
3. One-shot CLI: `issue-handler --once [--issue-number <id>]`.

## Flow

1. Architect-first gate: until the issue body contains the
   `ai-fabric:solution-architect` block, only the architect stage runs.
   The developer starts on a later cycle, so the plan stays reviewable.
2. Developer: worktree under `var/agents/issue-<N>`, agent run with the
   cheapest non-fast composer model, local checks
   (`fmt.sh && lint.sh && test.sh`), commit, push, PR with `Closes #N`.
3. CI failure on the PR: developer fixer reproduces the failure locally and
   pushes a fix — at most `2` attempts per head SHA, `6` per PR. When the
   budget is exhausted the architect posts a one-time design review, the PR
   and the linked issue get `status:needs_human`, and automation stops.
4. Merge: linked issue is labeled `status:completed`.

## Loop prevention

- `status:in_progress` is a lock: the poller skips such issues unless the
  claim (stored in the status comment) is older than
  `ISSUE_IN_PROGRESS_TIMEOUT_SEC` (default 3600).
- An in-process busy map prevents concurrent processing of the same
  issue/PR by overlapping cycles and webhooks.
- Webhook events for issues created by the fabric's own users are ignored.
- Reviews are deduplicated per PR head SHA; CI fixes are budgeted per SHA and
  per PR.
- Attempt counting lives in the machine-readable status comment, not in
  comment text matching.

## Status visibility

- Labels are the primary signal: `status:in_progress`, `status:pr_opened`,
  `status:failed`, `status:failed_max_attempts`, `status:needs_human`,
  `status:completed`, `status:cancelled`, `status:dry_run`.
- One bot-owned status comment per issue/PR is edited in place. It contains a
  hidden YAML state block (stage, attempts, claimed_at, ci_fix, reviewed_shas)
  plus a short human-readable summary. Progress does not spam comments.
- Durable events only as separate comments: architect escalation, automated
  review.

## Configuration

- `ISSUE_POLL_INTERVAL_SEC` (default `45`)
- `ISSUE_MAX_FIX_ATTEMPTS` (default `3`, local check-fix loop per run)
- `ISSUE_RETRY_INTERVAL_SEC` (default `600`, cooldown for `status:failed`)
- `ISSUE_IN_PROGRESS_TIMEOUT_SEC` (default `3600`, stale claim reclaim)
- `ISSUE_HANDLER_DRY_RUN` (`1` for safe dry-run)
- `ISSUE_SMART_MODEL` (explicit agent model; default: first non-fast
  `composer*` model from `agent --list-models`, else `auto`)
- `ISSUE_ARCHITECT_ENABLED` (default `1`), `ISSUE_ARCHITECT_MAX_CHARS` (default `6000`)
- `GITEA_WEBHOOK_SECRET` (HMAC validation of incoming webhooks)
- `TELEGRAM_BOT_TOKEN` (optional, start notifications)

## State

- Worktrees: `var/agents/issue-<N>`, `var/agents/pr-<N>`, `var/agents/pr-<N>-review`.
- Source of truth: Gitea labels + the YAML status comment. No local state files.
