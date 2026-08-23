# Project Directory Structure

Canonical reference for the AI Fabric repository layout. Use this document when onboarding, placing new code, or deciding where runtime state belongs.

For system design and data flows, see [`ai-fabric-poc.md`](ai-fabric-poc.md). For agent operating rules and module boundaries, see [`../skills/agent-guidelines.md`](../skills/agent-guidelines.md).

## Top-Level Overview

```
.
├── bin/              # Operational scripts and CI quality gates
├── cmd/              # Go application entrypoints (one binary per directory)
├── internal/         # Private application packages (not importable externally)
├── pkg/              # Reusable domain and system packages
├── var/              # Runtime-only state (gitignored, not source context)
├── docs/             # Architecture, workflows, skills, and plans
├── .gitea/           # Gitea Actions workflows and PR templates
├── docker-compose.yml
├── Dockerfile
└── go.mod
```

## `bin/` — Operational Scripts

Bash-first scripts that drive local development, CI gates, stack lifecycle, and release automation. They are the deterministic policy layer referenced by Gitea Actions.

| Script | Purpose |
| --- | --- |
| `fmt.sh` | Format Go code |
| `lint.sh` | Run linters |
| `test.sh` | Run the test suite |
| `review_policy.sh` | Enforce review policy checks |
| `pr_policy.sh` | Validate PR structure and metadata |
| `bootstrap.sh` | Initial environment setup |
| `up.sh` / `down.sh` | Start/stop the Docker Compose stack |
| `healthcheck.sh` | Stack health verification |
| `build_artifact.sh` | Build release tarball (`dist/ai-fabric-vX.Y.Z.tar.gz`) |
| `semantic_version.sh` | Wrapper for the `semantic-version` CLI |
| `fetch_doc.sh` | Wrapper for the `fetch-doc` CLI |
| `gitea-runner-command.sh` | Gitea Actions runner entrypoint |

All push and PR events must pass the baseline gates documented in [`../workflows/ci-cd.md`](../workflows/ci-cd.md):

1. `bin/fmt.sh`
2. `bin/lint.sh`
3. `bin/test.sh`
4. `bin/review_policy.sh`

## `cmd/` — Application Entrypoints

Each subdirectory under `cmd/` builds one binary. Entrypoints stay thin: parse flags, load configuration, wire dependencies, and call into `pkg/` or `internal/`.

| Directory | Binary | Role |
| --- | --- | --- |
| `cmd/issue-handler/` | `issue-handler` | Polls Gitea issues, tracks processing state, posts workflow comments. Compose service: `issue-handler`. |
| `cmd/bot/` | `tg-bot` | Telegram bot for task creation and delivery control. Compose service: `tg-bot`. |
| `cmd/semantic-version/` | `semantic-version` | Calculates the next semver tag from Conventional Commits. Used by CI on `main`. |
| `cmd/fetch-doc/` | `fetch-doc` | Downloads a URL to a local file path. Used by documentation capture workflows. |

Tests live alongside entrypoints (for example `cmd/issue-handler/main_test.go`).

## `internal/` — Private Application Code

Packages under `internal/` are private to this module. External consumers cannot import them.

| Package | Purpose |
| --- | --- |
| `internal/config` | Configuration loading from environment variables and YAML. Shared by `issue-handler` and `tg-bot`. |

Place new code here when it is application-specific and should not be exposed as a reusable library.

## `pkg/` — Reusable Packages

Domain and system packages shared across `cmd/` entrypoints. These follow standard Go layout conventions and may be imported by any binary in the module.

| Package | Purpose |
| --- | --- |
| `pkg/env` | Environment variable unmarshaling helpers |
| `pkg/fabric` | Core issue-handler logic (Gitea/Telegram interactions, state transitions) |
| `pkg/file` | Filesystem utilities, including module root path detection |
| `pkg/gitea` | Gitea client abstraction (CLI-first with SDK fallback) |

Keep business logic in `pkg/` rather than in `cmd/` entrypoints. Co-locate `_test.go` files with the code they cover.

## `var/` — Runtime State

**Runtime-only data. Not source context.**

The `var/` directory is gitignored. Agents and contributors must not treat its contents as implementation reference. Do not commit files from `var/`.

| Path | Purpose |
| --- | --- |
| `var/gitea/` | Gitea persistent data (Compose volume for the `gitea` service) |
| `var/runner-1/`, `var/runner-2/` | Gitea Actions runner state (when using named runner volumes) |
| `var/issue-handler/state.json` | Issue-handler processing state (attempts, status, last attempt) |
| `var/agents/issue-<N>/` | Ephemeral issue worktrees created during agent execution |

Related runtime artifacts at the repository root (also gitignored):

- `.issue-agent-prompt.md` — generated agent prompt for issue worktrees
- `.issue-architect-prompt.md` — generated architect prompt for issue worktrees

See [`../workflows/issue-handler.md`](../workflows/issue-handler.md) for issue-handler state rules and idempotency.

## Other Notable Directories

| Path | Purpose |
| --- | --- |
| `docs/architecture/` | System design documents (this file, PoC architecture) |
| `docs/workflows/` | CI/CD, issue automation, and PR workflow rules |
| `docs/skills/` | Agent runbooks and working agreements |
| `docs/plans/` | Accepted plans and ADR-like decisions |
| `docs/sources/` | Curated external references |
| `.gitea/workflows/` | Gitea Actions CI pipelines |
| `.env.example` | Required environment variable template (copy to `.env`) |

## Placement Guidelines

When adding new code or artifacts, use these rules:

1. **New binary?** Create `cmd/<name>/main.go` with minimal wiring; put logic in `pkg/` or `internal/`.
2. **Shared across binaries?** Add to `pkg/<domain>/`.
3. **Application-private?** Add to `internal/<package>/`.
4. **Operational script or CI gate?** Add to `bin/` and register it in [`../workflows/ci-cd.md`](../workflows/ci-cd.md) if it becomes a required gate.
5. **Runtime or generated state?** Write under `var/` or mark as ephemeral at the repo root; never commit.
6. **Documentation?** Add under the appropriate `docs/` subdirectory and link from [`../README.md`](../README.md).

## Related Documentation

- [`../workflows/ci-cd.md`](../workflows/ci-cd.md) — CI gate scripts and release policy
- [`../workflows/issue-handler.md`](../workflows/issue-handler.md) — issue-handler runtime flow and `var/issue-handler/` state
- [`../skills/agent-guidelines.md`](../skills/agent-guidelines.md) — module boundaries and context hygiene
- [`../skills/issue-handler-cli.md`](../skills/issue-handler-cli.md) — issue-handler service structure example
