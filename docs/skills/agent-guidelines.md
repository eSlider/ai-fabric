# Agent Guidelines

## Working Style

- Prefer minimal, reversible changes.
- Prefer CLI tools and Bash scripts when possible.
- Prefer CLI tools over MCP integrations or direct API calls whenever feasible.
- Avoid adding dependencies unless essential.
- Use `ruff` as the Python linter standard in CI.
- For Python environments and package workflows, prefer `uv venv` and `uv pip`.
- For browser testing, prefer `agent-browser` over heavyweight MCP browser tooling.

## Context Hygiene

- Read `docs/README.md` first.
- Keep `var/` out of context windows and version control.
- Store external references under `docs/sources/`.
- Respect ignore boundaries from `.gitignore` and `.ignore`.
