# AGENTS.md — AI Fabric

This repository is the **single public source of truth for decisions,
workflows, and agent skills** — plus the scripts that distribute skills across
systems. It deliberately contains no application code; the Go delivery fabric
was removed to keep this branch purely for decisions/workflows/skills + reuse.

## How this repo is organized

- [`decisions/`](decisions/README.md) — ADRs and accepted plans (the durable
  "why"). Includes architecture ADRs and curated external reference sources.
- [`workflows/`](workflows/README.md) — reusable process runbooks.
- [`skills/`](skills/README.md) — the consolidated skill catalog. Each skill is
  a subdir with a `SKILL.md` (+ aux files) and opencode-compatible frontmatter.
- [`sync.sh`](sync.sh) — idempotent installer that distributes every skill from
  `skills/` into a target opencode skills directory
  (`~/.config/opencode/skill/` by default).

## Reuse across systems (the intent)

Skills are **portable**. They are not edited in their installed locations;
`skills/` here is canonical. To install or refresh skills on any machine:

```bash
./sync.sh            # install/update all skills
./sync.sh --dry-run  # preview what would change
./sync.sh --target <dir>  # install into a custom dir
```

`sync.sh` is idempotent and mirrors the catalog (rsync `--delete`), so removing
a skill from `skills/` also removes it from the target.

## Rules (apply to every change in this repo)

- **Skill hygiene**: never edit a skill inside `~/.config/opencode/skill/`.
  Edit in `skills/<name>/` and re-run `sync.sh`.
- **No secrets**: never commit tokens, `.env`, or keys. `sync.sh` copies only
  the catalog contents; target credentials stay out of the repo.
- **No duplicate skills**: if two systems need the same skill, it lives once in
  `skills/`. Same-named skills are deduped (see `skills/README.md`).
- **No application code**: this branch is for decisions, workflows, skills, and
  distribution tooling only.
- **Docs reflect behaviour**: any change updates the relevant `*README.md` and
  this file.

## Primary reading order

1. [`README.md`](README.md) — what this is and how to use it.
2. [`decisions/README.md`](decisions/README.md) — ADR format + decisions.
3. [`skills/README.md`](skills/README.md) — catalog index + dedup decisions.
4. [`workflows/README.md`](workflows/README.md) — process runbooks.
