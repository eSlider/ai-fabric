# AI Fabric (PoC)

AI Fabric is a self-hosted, CLI-first automation workspace for software delivery.
It combines Gitea, Gitea Actions, and agent-driven automation so ideas can move
from task -> issue -> PR -> merge with quality gates enforced.

## Project Purpose

- Keep planning, code, issues, and pull requests in one local platform (Gitea).
- Automate repetitive delivery work with AI agents while keeping human approval points.
- Enforce backpressure with lightweight checks: `fmt`, `lint`, `test`, and PR policy.
- Stay simple and auditable: Bash/Go scripts, Docker Compose, minimal dependencies.

## Structure

- `docs/` architecture, workflows, skills, plans, and fetched sources
- `bin/` operational and CI helper scripts
- `var/` runtime data (ignored from git and agent context)
- `.gitea/workflows/` Gitea Actions pipelines

## Quick Start

1. Copy env:
   - `cp .env.example .env`
2. Start stack:
   - `./bin/up.sh`
3. Open Gitea:
   - `http://localhost:3000`

## Telegram Bot Control

Telegram bot Python service was removed and is pending Go replacement.

## Issue Handler Automation

Issue handler Python service was removed and is pending Go replacement.

Authentication note:
- Preferred: keep `CURSOR_AGENT_HOME_DIR` mounted so containerized `agent` reuses host login/session state.
- Fallback: set `CURSOR_API_KEY` in `.env` for non-interactive authentication.
- Gitea operations are executed via CLI (`tea api`) when `GITEA_CLI_ENABLED=1`; HTTP transport is used only when CLI mode is disabled.

## Self-Update And Redeploy Flow

1. Create work via Telegram `/task` (or directly in Gitea issue tracker).
2. `issue-handler` runs Solution Architect, posts options/estimation/impact, then waits for approval when required.
3. After approval, developer agent implements changes in a branch, runs checks, and opens a PR.
4. After PR merge to `main`, code is updated in this repository.
5. Restart services from the same repo checkout to load updated process code:
   - `./bin/down.sh && ./bin/up.sh`
   - or from Telegram: `/down` then `/up`
6. Runtime service stack is being migrated to Go entrypoints.

## Notes

- Default DB mode is SQLite for fast local PoC startup.
- Runtime state is stored under `var/` (`gitea`, `runner-1`, `runner-2`, issue handler state).
- Go source/build/test scope is `cmd/` and `pkg/` only; avoid `go test ./...` because `var/` is runtime-only data.
- Two Actions runners are configured: `gitea-runner-1` and `gitea-runner-2`.
- PR process is workflow-enforced via `.gitea/PULL_REQUEST_TEMPLATE.md`, `CODEOWNERS`, and `bin/pr_policy.sh`.
