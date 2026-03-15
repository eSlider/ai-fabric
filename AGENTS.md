# AGENTS

Primary guidance is in:

- `docs/README.md`
- `docs/skills/agent-guidelines.md`
- `docs/workflows/ci-cd.md`
- `docs/workflows/issue-handler.md`

Context boundary:

- Treat `var/` as runtime-only data.
- Do not use `var/` as source context for implementation tasks.

Project intent:

- This repository is an AI software-delivery fabric around Gitea, Actions runners, Telegram bot control, and automated issue handling.
- Agents should keep changes minimal, testable, and aligned with existing workflow/policy docs.

Runtime artifacts:

- `.issue-agent-prompt.md` and `.issue-architect-prompt.md` are generated prompts for issue worktrees.
- They are ephemeral runtime files and must not be committed.
