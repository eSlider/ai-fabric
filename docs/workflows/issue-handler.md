# Issue Handler Workflow

## Purpose

Automatically process each open issue by delegating implementation to an agent, then opening a PR with changes.

## Runtime

- Script: `bin/issue_handler.py`
- Launcher: `bin/issue_handler.sh`
- Service: `issue-handler` in `docker-compose.yml`
- Telegram `/task` creation path can trigger immediate one-shot processing for the created issue.
- Role skill docs:
  - `docs/skills/solution-architect.md`
  - `docs/skills/developer.md`

## Flow

1. Poll open issues from Gitea (`state=open`).
2. Skip issues already marked with open PR state in local handler state.
3. Classify issue type (`bug` or `feature`).
4. Select relevant skills/docs context from issue text.
5. Run **Solution Architect stage** with `cursor-agent`.
6. Update issue body directly with:
   - possible solutions
   - recommended approach
   - estimation
7. Prepare isolated git worktree (`issue/<id>-<slug>`).
8. Run developer agent (default `cursor-agent`) with generated prompt.
9. Run quality gates:
   - `./bin/fmt.sh`
   - `./bin/lint.sh`
   - `./bin/test.sh`
10. If checks fail, ask agent to fix and retry until `ISSUE_MAX_FIX_ATTEMPTS`.
11. Commit, push branch, create PR, comment URL back to issue.

## Configuration

Environment variables:

- `ISSUE_BASE_BRANCH` (default `main`)
- `ISSUE_POLL_INTERVAL_SEC` (default `45`)
- `ISSUE_MAX_FIX_ATTEMPTS` (default `3`)
- `ISSUE_AGENT_BIN` (default `cursor-agent`)
- `ISSUE_AGENT_EXTRA_ARGS`
- `ISSUE_HANDLER_DRY_RUN` (`1` for safe dry-run)
- `ISSUE_ARCHITECT_ENABLED` (`1` by default)
- `ISSUE_ARCHITECT_MAX_CHARS` (max architect text persisted into issue body)
- `ISSUE_HANDLER_TRIGGER_ON_CREATE` (`1` by default, used by Telegram bot for immediate trigger)
- `CURSOR_SETTINGS_DIR` and `CURSOR_CONFIG_DIR` (mounted into handler container for Cursor agent settings)

## Manual Trigger

- Process one specific issue immediately:
  - `./bin/issue_handler.sh --once --issue-number <id>`
- Process existing open issues once:
  - `./bin/issue_handler.sh --once`

## State

- Persistent state file: `var/issue-handler/state.json`
- Worktrees: `var/agents/issue-<number>/`
- Idempotency rules:
  - Solution Architect section is inserted once using body markers.
  - Existing marker block prevents architect re-run.
  - Tracked issues with `in_progress|pr_opened|failed` are not auto-retriggered, even if issue body is edited.
