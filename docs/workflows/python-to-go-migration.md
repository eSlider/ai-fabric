# Python to Go Migration

## Purpose

This document defines parity contracts and rollout checks for replacing legacy Python runtime/tooling with Go implementations.

## Scope

- Runtime services:
  - `issue-handler` -> `cmd/issue-handler`
  - `telegram-bot` -> `cmd/bot`
- Tooling:
  - semantic version resolver -> `cmd/semantic-version`
  - document fetch utility -> `cmd/fetch-doc`

## Parity Contracts

## Current Status

### Implemented in Go

- Runtime entrypoints:
  - `cmd/issue-handler/main.go`
  - `cmd/bot/main.go`
- Tooling entrypoints:
  - `cmd/semantic-version/main.go`
  - `cmd/fetch-doc/main.go`
- Compose services:
  - `issue-handler` (Dockerfile-based)
  - `tg-bot` (Dockerfile-based)

### Still Evolving

- Deep issue-handler orchestration parity from legacy Python flow:
  - architect/developer staged prompting
  - worktree-based implementation loop
  - full PR lifecycle automation

### Issue Handler

- Poll open issues from Gitea.
- Classify issue text (`bug` or `feature`).
- Persist handler state under `var/issue-handler`.
- Use Gitea transport with tea-first and SDK fallback.
- Support one-shot mode and continuous polling mode.

### Telegram Bot

- Respond to core operational commands:
  - `/status`, `/health`, `/up`, `/down`, `/checks`, `/projects`, `/logs`, `/task`
- Restrict access using allowlists when configured.
- Create Gitea issues from `/task`.
- Query project list from Gitea.

### Tooling

- Semantic version command:
  - Detect latest `vX.Y.Z` tag.
  - Compute bump from commit messages.
- Fetch-doc command:
  - Download URL content and write to target path.

## Compose Runtime Contract

- Bot and issue-handler services are Dockerfile-based.
- Compose watch is enabled for both services with rebuild on repository changes.
- Service defaults use:
  - `user: ${DOCKER_UID:-1000}:${DOCKER_GID:-1000}`

- 
  - `restart: unless-stopped`
  - `network_mode: host`
  - json-file logging with rotation.

## Validation Gates

- `go test ./cmd/... ./pkg/...`
- `docker compose config`
- `bin/lint.sh`
- `bin/test.sh` (subject to repository policy prerequisites such as `CODEOWNERS`)

## Rollout Runbook

1. Update `.env` from `.env.example` with runtime tokens and IDs.
2. Build and start stack:
  - `docker compose up -d --build`
3. Verify service health:
  - `docker compose ps`
  - `docker compose logs -f tg-bot`
  - `docker compose logs -f issue-handler`
4. Execute functional smoke checks:
  - Bot command response (`/status`, `/health`)
  - Issue creation from `/task`
  - Issue-handler one-shot (`issue-handler --once`)
5. Monitor first production cycle and runner stability.

## Rollback

1. Revert migration commit(s).
2. Restore previous compose service definitions.
3. Redeploy:
  - `docker compose up -d --build`

