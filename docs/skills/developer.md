# Developer Skill Guide

## Mission

Implement issue requirements into working code with minimal, safe changes.

## Required Workflow

1. Read issue + Solution Architect section in issue body.
2. Apply tests-first mindset where feasible.
3. Keep changes scoped to issue intent.
4. Run local gates:
   - `./bin/fmt.sh`
   - `./bin/lint.sh`
   - `./bin/test.sh`
5. Commit and push branch for PR.

## Coding Rules

- Prefer Bash/CLI-first solutions.
- Prefer existing project patterns and scripts.
- Keep implementation simple (KISS), avoid overengineering.
- Respect PR policy and template requirements.
