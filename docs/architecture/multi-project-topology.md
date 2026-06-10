# Multi-Project Topology (growth path)

Target: ai-fabric grows into a self-improving, multi-project agent assistant —
ultimately a "Jarvis"-style assistant reachable through a synapse-matrix bot
(including voice calls). This document fixes the repository topology decisions
so growth does not require restructuring later.

## Gitea organisations per direction

| Org | Purpose | Contents |
|-----|---------|----------|
| `ai-fabric` | The fabric itself | orchestrator service (issue handler, webhook server), agent role definitions, runner/compose infrastructure |
| `ai-libs` | Reusable libraries | extracted packages consumed via Go modules: gitea client, agent runner, prompt building, status/markers protocol |
| `assistant` (later) | Jarvis-facing services | matrix-synapse bot, voice gateway, conversation orchestration |
| one org per managed external direction | Projects the fabric works on | the fabric operates on these repos through the same issue/PR flow |

Note: today the fabric lives under the `eslider` user account on the local
Gitea; the org split applies when a second project direction appears (YAGNI —
do not create empty orgs in advance).

## Repository rules

- A repository is either a **library** or a **deployable service/program** —
  never both.
- Libraries get semver tags so the fabric can update consumers through its own
  PR flow.
- Extraction trigger is the rule of three (see
  `docs/skills/engineering-principles.md`): the third copy of a fragment
  across repos forces extraction into `ai-libs`.

## Multi-repo readiness in code

- Webhook-driven paths (fixer, reviewer, architect trigger) take owner/repo
  from the webhook payload, not from global config — they already work for any
  repo that points its webhook at the handler.
- The poller is intentionally single-repo (`GITEA_BOT_OWNER`/`GITEA_BOT_REPO`)
  until a second managed repo exists.
- Per-role Gitea users (`ai-architect`, `ai-developer`, `ai-reviewer`) are
  instance-wide: granting them collaborator access to a repo onboards that
  repo to the fabric.

## Self-improvement loop

- The fabric improves itself through its own issue flow: issues on the
  `ai-fabric` repo are processed by the fabric, governed by the engineering
  principles and the loop budgets (fix attempts per SHA/PR, architect
  escalation, `status:needs_human` terminal state).
- Issues created by the fabric's own users do not auto-trigger processing —
  a human (or an explicitly allowed channel like the Telegram bot) promotes
  them, which keeps the self-improvement loop bounded.
