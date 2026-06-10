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

1. Polling: every `ISSUE_POLL_INTERVAL_SEC` (default 45s) all open issues are
   scanned and conflicted open PRs are healed.
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
   pushes a fix — at most `2` pushed fixes per head SHA, `6` per PR (only a
   pushed fix consumes budget; failed or no-op attempts do not). When the
   budget is exhausted the architect posts a one-time design review, the PR
   and the linked issue get `status:needs_human`, and automation stops.
4. Review loop: the reviewer separates findings into in-scope and
   out-of-scope. On REQUEST_CHANGES the developer addresses the in-scope
   findings and pushes (at most `ISSUE_REVIEW_FIX_MAX_PER_PR` rounds, then
   `status:needs_human`); the new head is reviewed again. Out-of-scope
   findings are filed once per PR as a separate `[follow-up]` issue that
   enters the normal architect-developer flow.
5. Conflicts: the poller detects open PRs that became unmergeable after the
   base moved; the developer agent merges the base into the head, resolves
   conflicts and pushes (which re-runs CI and review).
6. Merge: the linked issue gets `status:completed`, the architect's "## Tasks"
   checklist in the issue body is ticked, and the issue is closed unless
   another open PR still references it.

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
- `ISSUE_AGENT_TIMEOUT_SEC` (default `1800`, hard deadline per agent run)
- `ISSUE_HANDLER_DRY_RUN` (`1` for safe dry-run)
- `ISSUE_SMART_MODEL` (explicit agent model; default: first non-fast
  `composer*` model from `agent --list-models`, else `auto`)
- `ISSUE_ARCHITECT_ENABLED` (default `1`), `ISSUE_ARCHITECT_MAX_CHARS` (default `6000`),
  `ISSUE_ARCHITECT_MAX_ATTEMPTS` (default `2`, then `status:needs_human`)
- `ISSUE_REVIEW_FIX_MAX_PER_PR` (default `3`, developer rounds per PR to
  satisfy the reviewer before `status:needs_human`)
- `GITEA_WEBHOOK_SECRET` (HMAC validation of incoming webhooks; required in
  daemon mode — the handler refuses to start without it)
- `TELEGRAM_BOT_TOKEN` (optional, start notifications)

## State

- Worktrees: `var/agents/issue-<N>`, `var/agents/pr-<N>`, `var/agents/pr-<N>-review`.
- Source of truth: Gitea labels + the YAML status comment. No local state files.
