# AI Fabric (PoC)

Lean AI project fabric based on Gitea + Gitea Actions, optimized for:

- code/issues/PRs in one place
- automated backpressure (`fmt`, `lint`, `test`)
- Bash/CLI-first workflows to keep complexity and token usage low

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
2. Start bot:
   - `./bin/telegram_bot.sh`
3. Commands:
   - `/status`, `/health`, `/up`, `/down`, `/checks`, `/logs <service>`
   - `/task <description>` to classify and create a clear Gitea issue via follow-up questions

## Notes

- Default DB mode is SQLite for fast local PoC startup.
- Optional PostgreSQL profile is available in `docker-compose.yml`.
- Two Actions runners are configured: `gitea-runner-1` and `gitea-runner-2`.
- PR process is workflow-enforced via `.gitea/PULL_REQUEST_TEMPLATE.md`, `CODEOWNERS`, and `bin/pr_policy.sh`.
