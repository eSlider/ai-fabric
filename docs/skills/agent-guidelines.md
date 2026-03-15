# Agent Guidelines

## Working Style

- Prefer minimal, reversible changes.
- Prefer CLI tools and Bash scripts when possible.
- Avoid adding dependencies unless essential.

## Context Hygiene

- Read `docs/README.md` first.
- Keep `var/` out of context windows and version control.
- Store external references under `docs/sources/`.
- Respect ignore boundaries from `.gitignore` and `.ignore`.
