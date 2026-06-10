# Agent Guidelines

Read `docs/skills/engineering-principles.md` first — its rules are inviolable
and override anything below in case of conflict.

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
- For Gitea operations in Go code, use the typed `pkg/gitea` client (official SDK types). In shell flows, plain `curl` against the REST API is fine.
- Keep comments and docs concise, in English, and aligned with current behavior.

## Testing Policy: no mocks, no unit tests

- Mock-based and isolated unit tests are forbidden — they camouflage problems. A test that mocks a collaborator is a bug.
- Only use-case tests, API tests (real HTTP), and system tests (`//go:build system`) are allowed. See `docs/skills/engineering-principles.md` section 6.
- Behavior-first: write the use-case test against the real stack before implementing.

## CI Expectations

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
