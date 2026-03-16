# Issue Handler Workflow

## Purpose

Automatically process each open issue by delegating implementation to an agent, then opening a PR with changes.

## Runtime

- Go runtime command: `cmd/issue-handler/main.go`
- Compose service: `issue-handler`
- Telegram `/task` creation path can trigger immediate one-shot processing for the created issue.
- Role skill docs:
  - `docs/skills/solution-architect.md`
  - `docs/skills/developer.md`

## Flow

1. Poll open issues from Gitea (`state=open`).
2. If `--issue-number` is set, process only that issue when open and not a PR.
3. Load persisted state from `var/issue-handler/state.json`.
4. Skip terminal statuses: `completed`, `failed_max_attempts`, `pr_opened`, `cancelled`.
5. Enforce retry cooldown for `failed` status using `ISSUE_RETRY_INTERVAL_SEC`.
6. Parse Telegram marker in issue body and send a start notification when available.
7. In normal mode:
   - add claim/progress comments to the issue
   - update local state (`attempts`, `last_attempt`, `status=completed`)
8. In dry-run mode (`ISSUE_HANDLER_DRY_RUN=1`):
   - do not mutate Gitea issue state
   - only update local state with `status=dry_run`

## Current vs Target

- Current implementation is a Go-first processing loop with state handling and Gitea transport abstraction.
- Full architect/developer worktree automation and PR lifecycle parity are tracked in:
  - `docs/workflows/python-to-go-migration.md`

## Configuration

Environment variables:

- `ISSUE_POLL_INTERVAL_SEC` (default `45`)
- `ISSUE_MAX_FIX_ATTEMPTS` (default `3`)
- `ISSUE_RETRY_INTERVAL_SEC` (default `600`, retry delay for failed issues)
- `ISSUE_HANDLER_DRY_RUN` (`1` for safe dry-run)
- `TELEGRAM_BOT_TOKEN` (optional; enables Telegram notifications from handler)
- `GITEA_CLI_ENABLED` (`1` by default, prefer CLI transport for Gitea operations)
- `GITEA_CLI_BIN` (optional local CLI binary path/name; when empty, dockerized `tea` is used)
- `GITEA_CLI_IMAGE` (default `gitea/tea:latest`, used for dockerized CLI mode)
- `GITEA_CLI_LOGIN` (default `ai-fabric`, login alias used by `tea`)
- `GITEA_CLI_URL` (Gitea URL used by CLI login)
- `GITEA_CLI_TOKEN` (token used by CLI login; defaults to `GITEA_BOT_TOKEN` when empty)
- `GITEA_CLI_DOCKER_NETWORK` (default `host`, network mode for dockerized CLI)
- `GITEA_TRANSPORT_PRIMARY` / `GITEA_PRIMARY_TRANSPORT` (`cli` or `sdk`)
- `GITEA_CLI_FALLBACK_ENABLED` / `GITEA_TRANSPORT_CLI_FALLBACK` (enable secondary transport fallback)

## Manual Trigger

- Process one specific issue immediately:
  - `issue-handler --once --issue-number <id>`
- Process existing open issues once:
  - `issue-handler --once`

## State

- Persistent state file: `var/issue-handler/state.json`
- Idempotency rules:
  - Terminal statuses are not reprocessed.
  - Failed issues respect retry interval.
