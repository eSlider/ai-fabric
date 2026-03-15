# AI Fabric PoC Architecture

## Goals

- Centralize project lifecycle in Gitea (code, issues, PRs).
- Run CI quality gates through Gitea Actions.
- Keep automation lightweight and inspectable (Bash-first).

## Components

- `gitea`: SCM, issues, PRs, Actions control plane.
- `gitea-runner-1` and `gitea-runner-2`: execute Actions jobs.

## Operational Layout

- Persistent state under `var/`:
  - `var/gitea/`
  - `var/runner-1/`
  - `var/runner-2/`
- Scripts in `bin/` drive lifecycle and checks.
