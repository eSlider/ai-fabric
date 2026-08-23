---
name: git-workflow
description: Git workflow for the <project> repo and owned go-* libraries — Gitea-only work, branches, Conventional Commits, PR → review → CI → merge into release/v1, SemVer tagging, merge-order discipline to avoid conflicts. Use for all git/PR/review operations.
---

# Git Workflow (<project> + owned go-* repos)

## Canonical home — Gitea only

- Canonical repo: `git.<host>/<owner>/<project>`. Owned libraries (`github.com/eSlider/go-*`) work on Gitea `git.<host>/eSlider/<repo>`. GitHub is a **publish mirror only**.
- Issues, PRs, reviews, epics, milestones, releases exist **only on Gitea** — never on GitHub.

## Branching

- Branch name: `type/slug#issue` (e.g. `feat/content-url-addressing#100`, `chore/jitsi-deploy#135`).
- Types: `feat`, `fix`, `refactor`, `chore`, `docs`, `test`.
- No direct pushes to `main`/`release/*`. Work happens on the branch; integration happens via PR.

## Commits

- Conventional Commits: `type(scope): summary (#id)` — e.g. `feat(jitsi): deploy jitsi meet stack (#135)`.
- The `(#id)` ref ties the commit to its Gitea issue; when merged it closes/references the issue.
- Never commit secrets, `.env`, or keys. Secrets live in gitignored files.

## HARD RULE — secret-scan BEFORE ANY PUSH

- **BEFORE ANY PUSH**, run the secret scanner (gitleaks) on the commits being
  pushed. Do NOT push if anything is found.
- Automatic guard: the repo installs a git **pre-push** hook (`scripts/githooks/`,
  install via `./scripts/githooks/install.sh`) that scans the new-commit diff with
  gitleaks and **blocks** the push (exit 1) on any leak. There is also a
  **pre-commit** hook for staged changes.
- Explicit command (fallback if the hook is unavailable):
  `gitleaks detect --source . --log-opts="<base-ref>..HEAD" --redact --verbose`
  e.g. `gitleaks detect --source . --log-opts="origin/release/v1..HEAD" --redact --verbose`
  Exit code != 0 ⇒ leaks found ⇒ **do not push**; remove the secret first.
- CI also runs the same scan on `pull_request` + `push` (fail-on-leak ⇒ red CI).

## PR → review → CI → merge

1. Push branch `type/slug#issue`.
2. Open a PR toward **`release/v1`** (not main).
3. **Review**: PO (or a senior reviewer) reviews the diff — quality, tests present and green, no secrets, no hardcoded absolute paths/host URLs, docs updated. Reviewer may REQUEST_CHANGES.
4. **CI must be green** before merge (Gitea Actions). If a change request or CI fails, fix on the same branch (new commit), re-verify, re-push.
5. **Merge** into `release/v1`. Then close the issue via the commit ref / issue comment with evidence.

## Merge-order discipline (avoid conflicts)

- When two PRs touch the same files/packages, merge one first, then **rebase** the other onto the new `release/v1` head and resolve the conflict **by design** (keep the intended seam/abstraction), not by guessing.
- Track merge-order before delegating parallel tasks so they don't collide at merge time.
- After a merge, other open PRs touching the same files may become `mergeable=false` — rebase them.

## SemVer tagging

- After a merge to `release/v1`, tag on Gitea:
  - `fix:` → PATCH
  - `feat:` → MINOR
  - breaking (`!`) → MAJOR
- Next version computed by the semver tool from the previous tag.

## Gitea API access

- Token in `~/.tea/tea.yml` (field `token:`). API base `https://<gitea-host>/api/v1`.
- Issue/PR/comment/review operations via API; issue text and comments on Gitea in Russian.
