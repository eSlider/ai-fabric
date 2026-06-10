# Skill: Issue Handler CLI (Go)

The `issue-handler` is a Go service that polls Gitea issues, serves webhooks,
and drives the architect/developer/reviewer agent pipeline.

## Project Structure

- `cmd/issue-handler/main.go`: Minimal entry point, handles CLI flags, starts the poll loop and the webhook server on `:8082`.
- `internal/config/config.go`: Configuration loading using reflection-based environment variable mapping.
- `pkg/fabric/issue_handler.go`: Core orchestration (architect gate, developer stage, status protocol).
- `pkg/fabric/webhook_handler.go`: Webhook routing (issues, pull_request, status), CI fixer, reviewer, architect escalation.
- `pkg/fabric/status.go`: Single editable status comment with hidden YAML state.
- `pkg/gitea/client.go`: Typed Gitea client built on the official SDK (`code.gitea.io/sdk/gitea`); SDK types are the domain types.
- `pkg/file/file.go`: Utility functions for file system operations, including root path detection.

## Usage

### Prerequisites

- Go 1.24+
- Gitea instance
- `agent` (Cursor Agent CLI) authenticated; credentials from `~/.config/cursor/auth.json` are mounted into the container

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GITEA_BOT_BASE_URL` | `http://localhost:3000` | Gitea API base URL |
| `GITEA_BOT_TOKEN` | (required) | Gitea API token (fallback for all roles) |
| `GITEA_BOT_OWNER` | `eslider` | Repository owner |
| `GITEA_BOT_REPO` | `ai-fabric` | Repository name |
| `GITEA_HANDLER_TOKEN` | bot token | Developer role user token (`ai-developer`) |
| `GITEA_REVIEWER_TOKEN` | bot token | Reviewer role user token (`ai-reviewer`) |
| `GITEA_ARCHITECT_TOKEN` | bot token | Architect role user token (`ai-architect`) |
| `GITEA_WEBHOOK_SECRET` | (empty) | HMAC secret for incoming webhooks |
| `ISSUE_BASE_BRANCH` | `main` | Base branch for worktrees |
| `ISSUE_POLL_INTERVAL_SEC` | `45` | Polling interval |
| `ISSUE_IN_PROGRESS_TIMEOUT_SEC` | `3600` | Stale `in_progress` reclaim timeout |
| `ISSUE_SMART_MODEL` | (auto-detect) | Agent model; default: first non-fast `composer*` model |
| `ISSUE_HANDLER_DRY_RUN` | `0` | If `1`, do not perform actual changes |

### Command Line Flags

- `--once`: Run a single polling cycle and exit (webhook server disabled).
- `--issue-number <int>`: Process only a specific issue number.

## Testing Strategy

No mocks, no isolated unit tests (see `docs/skills/engineering-principles.md`).

- API tests: `pkg/fabric/webhook_api_test.go` — real HTTP against the webhook endpoint.
- Use-case/system tests: `pkg/fabric/system_test.go` (`//go:build system`) — full issue flow against the live local Gitea with a deterministic stub agent process.

```bash
./bin/test.sh          # fast checks
./bin/system_test.sh   # system suite against live Gitea (loads .env)
```

## Workflow

1. **Polling/webhook**: open issues are scanned every cycle; `issues opened` webhooks trigger the architect immediately.
2. **Loop gates**: terminal labels short-circuit; `status:in_progress` acts as a lock with a staleness timeout; an in-process busy map prevents concurrent runs.
3. **Architect-first**: until the issue body carries the architect block, only analysis runs.
4. **Developer**: worktree, agent run (cheap composer model), local checks, commit/push as `ai-developer`, PR with `Closes #N`.
5. **CI failure**: commit-status webhook routes the failure to the matching PR; fixes are budgeted (2 per SHA, 6 per PR), then the architect escalates and `status:needs_human` stops automation.
6. **Status**: one editable bot comment with hidden YAML state; labels carry the coarse status; merge sets `status:completed`.
