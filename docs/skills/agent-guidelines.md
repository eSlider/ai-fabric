# Agent Guidelines

## Architecture And Boundaries

- Read `docs/README.md` first, then workflow docs before implementation.
- Treat `var/` as runtime-only data; never use it as source context for code changes.
- Keep implementation scoped to existing module boundaries (`cmd/` entrypoints, `pkg/` reusable logic, `bin/` operational scripts).
- Preserve runtime artifacts rules: `.issue-agent-prompt.md` and `.issue-architect-prompt.md` are ephemeral and must not be committed.

## Engineering Rules (Go First)

- Prefer Go for new implementation and refactors.
- Use Python only when there is no viable Go path; if Python is required, use `uv` workflows (`uv venv`, `uv pip`).
- Prefer pure functions and small focused structs; avoid unnecessary abstractions.
- Keep dependencies minimal; standard library first.
- Wrap errors with context (`fmt.Errorf("context: %w", err)`); avoid panics in normal control flow.
- For I/O paths, prefer `context.Context` as the first parameter.

## Working Style

- Prefer minimal, reversible changes over broad rewrites.
- Keep behavior explicit and testable; avoid hidden side effects.
- Prefer CLI/Bash integration patterns already used in this repo.
- Keep comments and docs concise, in English, and aligned with current behavior.

## Testing And CI Expectations

- Follow test-first development for new behavior and bug fixes.
- Validate locally before proposing/merging changes:
  - `bin/fmt.sh`
  - `bin/lint.sh`
  - `bin/test.sh`
  - `bin/review_policy.sh`
- Respect PR policy requirements (`bin/pr_policy.sh`, PR template, CODEOWNERS).

## Operational Safety

- Do not introduce destructive git actions into automation flows.
- Preserve idempotency and state safety for issue-handler workflows.
- Do not commit secrets, tokens, or environment-specific credentials.
- Ensure runtime binaries and auth paths are resolvable when used in containerized flows.

## Context Hygiene

- Store curated external references under `docs/sources/`.
- Respect ignore boundaries from `.gitignore` and `.ignore`.
