# AI Fabric (PoC)

AI Fabric is a self-hosted, CLI-first automation workspace for software delivery.
It combines Gitea, Gitea Actions, a Telegram control bot, and agent-driven issue handling
so ideas can move from task -> issue -> PR -> merge with quality gates enforced.

## Project Purpose

- Keep planning, code, issues, and pull requests in one local platform (Gitea).
- Automate repetitive delivery work with AI agents while keeping human approval points.
- Enforce backpressure with lightweight checks: `fmt`, `lint`, `test`, and PR policy.
- Stay simple and auditable: Bash/Python scripts, Docker Compose, minimal dependencies.

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

1. Set in `.env`:
   - `TELEGRAM_BOT_NAME=trrrrmonexbot`
   - `TELEGRAM_BOT_TOKEN=<token>`
   - `TELEGRAM_ALLOWED_CHAT_IDS=<your_chat_id>`
   - `TELEGRAM_ALLOWED_USERNAMES=eSIider`
   - `GITEA_BOT_BASE_URL=http://localhost:3000`
   - `GITEA_BOT_OWNER=eslider`
   - `GITEA_BOT_REPO=ai-fabric`
   - `GITEA_BOT_TOKEN=<gitea_access_token>`
2. Bot service is part of Docker Compose (`telegram-bot`) and starts with:
   - `./bin/up.sh`
3. Commands:
  - `/status`, `/health`, `/up`, `/down`, `/checks`, `/projects`, `/logs <service>`
   - `/task <description>` to classify and create a clear Gitea issue via follow-up questions
   - Approval replies for Solution Architect output: option number, `confirm`, or `cancel`

## Issue Handler Automation

1. Configure in `.env`:
   - `GITEA_BOT_TOKEN=<gitea_access_token>`
   - `ISSUE_AGENT_BIN=agent`
   - `ISSUE_ARCHITECT_ENABLED=1`
   - `CURSOR_SETTINGS_DIR=${HOME}/.cursor`
   - `CURSOR_CONFIG_DIR=${HOME}/.config/Cursor`
   - `CURSOR_LOCAL_BIN_DIR=${HOME}/.local/bin`
   - `CURSOR_AGENT_HOME_DIR=${HOME}/.local/share/cursor-agent`
2. Start service:
   - `docker compose -f docker-compose.yml up -d issue-handler`
3. One-shot dry-run:
   - `ISSUE_HANDLER_DRY_RUN=1 ./bin/issue_handler.sh --once`

## Self-Update And Redeploy Flow

1. Create work via Telegram `/task` (or directly in Gitea issue tracker).
2. `issue-handler` runs Solution Architect, posts options/estimation/impact, then waits for approval when required.
3. After approval, developer agent implements changes in a branch, runs checks, and opens a PR.
4. After PR merge to `main`, code is updated in this repository.
5. Restart services from the same repo checkout to load updated Python process code:
   - `./bin/down.sh && ./bin/up.sh`
   - or from Telegram: `/down` then `/up`
6. `telegram-bot` and `issue-handler` containers run the updated repository code and continue serving commands/tasks.

## Notes

- Default DB mode is SQLite for fast local PoC startup.
- Runtime state is stored under `var/` (`gitea`, `runner-1`, `runner-2`, issue handler state).
- Two Actions runners are configured: `gitea-runner-1` and `gitea-runner-2`.
- PR process is workflow-enforced via `.gitea/PULL_REQUEST_TEMPLATE.md`, `CODEOWNERS`, and `bin/pr_policy.sh`.
