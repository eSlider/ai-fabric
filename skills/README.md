# Skills Catalog

Single consolidated catalog of agent skills for the fabric. This is the
**public mirror** of the canon (private Gitea `eSlider/inventar`,
`docs/skills/`); all systems that consume skills copy from here via
[`sync.sh`](../../sync.sh).

This showcase contains **generic, reusable skills only**. Host-specific
skills — anything tied to internal hosts, paths or domain tooling — stay in
the private canon `eSlider/inventar` (`docs/skills/`) and are **not** mirrored
here.

Each skill lives in its own subdirectory with a `SKILL.md` (plus any auxiliary
files). Skills carry opencode-compatible frontmatter (`name:`, `description:`)
so tooling can discover and load them.

## Roles & workflow

- [`po`](po/SKILL.md) — product owner role: priorities, acceptance, dispatch.
- [`se`](se/SKILL.md) — software engineer role: TDD implementation per issue.
- [`git-workflow`](git-workflow/SKILL.md) — Gitea-only work, Conventional Commits, PR flow, SemVer.
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

## Writing / domain style

- [`agent-cost`](agent-cost/SKILL.md) — agent cost accounting.
- [`caveman-docs`](caveman-docs/SKILL.md) — caveman-style agent chat docs.

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
| `web-search` | `<project>`, `agent-skills` | `<project>` | Newer (2026-08-21 vs 2026-08-10), portable config via env var, references `bin/web/search.go`; agent-skills version hard-coded a specific internal host — less portable. |
| `diataxis-docs` | `<project>`, `agent-skills` | `<project>` | Newer; references current `bin/brain/index.go`. |
| `po` / `se` / `git-workflow` | global `opencode`, `<project>` | identical content | Byte-identical copies verified via `diff -r`; kept the `<project>` copy. |

Other skills (`api-client`, `duckdb`, `effective-go`, `etl-handler`,
`picoclaw`, `postgres`, `yq`, `brain`) existed only in `<project>` and are unique.
`agent-cost` and `caveman-docs` existed only in `agent-skills` and are unique.

> Host-specific skills from the legacy merge are not part of this public
> showcase — they live in the private canon `eSlider/inventar` only.

## Maintenance

- Do **not** edit skills directly in system install dirs (e.g.
  `~/.config/opencode/skill/`) and do **not** edit them here — edit the canon
  (`eSlider/inventar`, `docs/skills/`), then mirror into this repo and run
  `../../sync.sh` to propagate.
- Adding a new skill = add one subdir here, list it above, then `sync.sh`.
