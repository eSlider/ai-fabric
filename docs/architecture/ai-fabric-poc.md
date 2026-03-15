# AI Fabric PoC Architecture

## Goals

- Centralize project lifecycle in Gitea (code, issues, PRs).
- Run CI quality gates through Gitea Actions.
- Keep automation lightweight and inspectable (Bash-first).

## Components

- `gitea`: SCM, issues, PRs, Actions control plane.
- `gitea-runner`: executes Actions jobs.
- optional `postgres`: alternative DB profile.

## Operational Layout

- Persistent state under `var/`:
  - `var/gitea/`
  - `var/runner/`
  - `var/postgres/` (if postgres profile enabled)
- Scripts in `bin/` drive lifecycle and checks.
