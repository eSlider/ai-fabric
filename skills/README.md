# Skills Catalog

Single consolidated catalog of agent skills for the fabric. This is the **one
public source of truth** for skills; all systems that consume skills copy from
here via [`sync.sh`](../../sync.sh).

Each skill lives in its own subdirectory with a `SKILL.md` (plus any auxiliary
files). Skills carry opencode-compatible frontmatter (`name:`, `description:`)
so tooling can discover and load them.

## Roles & workflow

- [`po`](po/SKILL.md) — product owner role: priorities, acceptance, dispatch.
- [`se`](se/SKILL.md) — software engineer role: TDD implementation per issue.
- [`git-workflow`](git-workflow/SKILL.md) — Gitea-only work, Conventional Commits, PR flow, SemVer.
- [`devops`](devops/SKILL.md) — devops sub-agent role (`RUNBOOK.md` alongside).
- [`api-client-design`](api-client-design/SKILL.md) — canonical API client architecture.

## Engineering / technical

- [`api-client`](api-client/SKILL.md) — API client implementation skill.
- [`brain`](brain/SKILL.md) — knowledge-brain / facts-narrative method (`references/tools.md`).
- [`duckdb`](duckdb/SKILL.md) — DuckDB usage skill.
- [`effective-go`](effective-go/SKILL.md) — idiomatic Go standards.
- [`etl-handler`](etl-handler/SKILL.md) — ETL handler design.
- [`picoclaw`](picoclaw/SKILL.md) — evidence audit before factual replies.
- [`postgres`](postgres/SKILL.md) — Postgres usage skill.
- [`yq`](yq/SKILL.md) — YAML processing via yq.
- [`web-search`](web-search/SKILL.md) — SearXNG web search (`references/instance-tuning.md`).
- [`diataxis-docs`](diataxis-docs/SKILL.md) — documentation classification by Diataxis.

## Domain

- [`agent-cost`](agent-cost/SKILL.md) — agent cost accounting.
- [`caveman-docs`](caveman-docs/SKILL.md) — caveman-style agent chat docs.
- [`domain-detective`](domain-detective/SKILL.md) — detective method for domain facts.
- [`db-yaml`](db-yaml/SKILL.md) — database YAML config skill.
- [`example-repos`](example-repos/SKILL.md) — repo conventions (`scripts/gitea.sh`).
- [`kb-search`](kb-search/SKILL.md) — knowledge-base search skill.

## Fabric operating skills (from legacy `docs/skills/`)

- [`engineering-principles`](engineering-principles/SKILL.md) — inviolable engineering rules.
- [`agent-guidelines`](agent-guidelines/SKILL.md) — global agent working agreements.
- [`lazy-senior-dev`](lazy-senior-dev/SKILL.md) — YAGNI/stdlib-first implementation mode.
- [`developer`](developer/SKILL.md) — implementation workflow.
- [`solution-architect`](solution-architect/SKILL.md) — issue-level solution analysis.
- [`issue-handler-cli`](issue-handler-cli/SKILL.md) — legacy issue-handler design reference.

## Dedup decisions

Consolidation merged several same-named skills that existed in multiple repos
(`<project>`, `agent-skills`, global `opencode`). The rule: **keep ONE copy — the
most recent and complete** — and record the choice here.

| Skill | Sources merged | Kept from | Reason |
|-------|---------------|-----------|--------|
| `web-search` | `<project>`, `agent-skills` | `<project>` | Newer (2026-08-21 vs 2026-08-10), portable config via env var, references `bin/web/search.go`; agent-skills version hard-coded a specific host (`search.<host>`) — less portable. |
| `diataxis-docs` | `<project>`, `agent-skills` | `<project>` | Newer; references current `bin/brain/index.go`. |
| `po` / `se` / `git-workflow` / `devops` | global `opencode`, `<project>` | identical content | Byte-identical copies verified via `diff -r`; kept the `<project>` copy. |

Other skills (`api-client`, `duckdb`, `effective-go`, `etl-handler`,
`picoclaw`, `postgres`, `yq`, `brain`) existed only in `<project>` and are unique.
`agent-cost`, `caveman-docs`, `domain-detective`, `db-yaml`,
`example-repos`, `kb-search` existed only in `agent-skills` and are
unique.

## Maintenance

- Do **not** edit skills directly in system install dirs (e.g.
  `~/.config/opencode/skill/`). Edit here and run `../../sync.sh` to propagate.
- Adding a new skill = add one subdir here, list it above, then `sync.sh`.
