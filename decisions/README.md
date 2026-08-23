# Decisions

Architecture Decision Records (ADRs) and accepted plans for the fabric. This is
where "we decided X because Y" lives — the durable rationale behind the
structure, workflows, and skills in this repo.

## Layout

- `architecture/` — system design, data flows, directory structure (ADRs).
- `plans/` — accepted implementation plans and decision snapshots.
- `reference/` — curated external sources captured in structured form (used as
  input to decisions).

## ADR format

Each ADR is a markdown file describing one decision. Suggested shape:

```markdown
# ADR-####: <title>

Status: proposed | accepted | deprecated | superseded
Date: <YYYY-MM-DD>

## Context
The situation that forced the decision.

## Decision
What we decided, in one paragraph.

## Consequences
What becomes easier or harder because of this decision (positive and negative).
```

Keep ADRs short and factual. If a decision is superseded, do not rewrite it —
mark it `superseded` and link the replacement so the history stays auditable.
