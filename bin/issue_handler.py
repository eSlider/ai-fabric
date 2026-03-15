#!/usr/bin/env python3
import argparse
import json
import os
import re
import shlex
import shutil
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


ROOT_DIR = Path(__file__).resolve().parent.parent
STATE_DIR = ROOT_DIR / "var" / "issue-handler"
STATE_PATH = STATE_DIR / "state.json"

GITEA_BASE_URL = os.environ.get("GITEA_BOT_BASE_URL", "http://localhost:3000").rstrip("/")
GITEA_OWNER = os.environ.get("GITEA_BOT_OWNER", "eslider")
GITEA_REPO = os.environ.get("GITEA_BOT_REPO", "ai-fabric")
GITEA_TOKEN = os.environ.get("GITEA_BOT_TOKEN", "").strip()
BASE_BRANCH = os.environ.get("ISSUE_BASE_BRANCH", "main")
POLL_INTERVAL = int(os.environ.get("ISSUE_POLL_INTERVAL_SEC", "45"))
MAX_FIX_ATTEMPTS = int(os.environ.get("ISSUE_MAX_FIX_ATTEMPTS", "3"))
AGENT_BIN = os.environ.get("ISSUE_AGENT_BIN", "cursor-agent").strip()
AGENT_EXTRA_ARGS = os.environ.get("ISSUE_AGENT_EXTRA_ARGS", "").strip()
DRY_RUN = os.environ.get("ISSUE_HANDLER_DRY_RUN", "0").strip() == "1"
ARCHITECT_ENABLED = os.environ.get("ISSUE_ARCHITECT_ENABLED", "1").strip() == "1"
ARCHITECT_MAX_CHARS = int(os.environ.get("ISSUE_ARCHITECT_MAX_CHARS", "6000"))

ARCH_START = "<!-- ai-fabric:solution-architect:start -->"
ARCH_END = "<!-- ai-fabric:solution-architect:end -->"


def _request_to_base(base_url: str, method: str, path: str, data: dict[str, Any] | None = None) -> Any:
    body = None
    headers = {"Authorization": f"token {GITEA_TOKEN}", "Content-Type": "application/json"}
    if data is not None:
        body = json.dumps(data).encode("utf-8")
    req = urllib.request.Request(f"{base_url}{path}", data=body, method=method, headers=headers)
    with urllib.request.urlopen(req, timeout=40) as resp:
        raw = resp.read().decode("utf-8")
        return json.loads(raw) if raw else {}


def gitea_request(method: str, path: str, data: dict[str, Any] | None = None) -> Any:
    if not GITEA_TOKEN:
        raise RuntimeError("GITEA_BOT_TOKEN is required")
    base_candidates = [GITEA_BASE_URL]
    if "://gitea:" in GITEA_BASE_URL:
        base_candidates.append(GITEA_BASE_URL.replace("://gitea:", "://localhost:"))
    elif "://localhost:" in GITEA_BASE_URL:
        base_candidates.append(GITEA_BASE_URL.replace("://localhost:", "://gitea:"))
    elif GITEA_BASE_URL.endswith("://gitea"):
        base_candidates.append(GITEA_BASE_URL.replace("://gitea", "://localhost"))
    elif GITEA_BASE_URL.endswith("://localhost"):
        base_candidates.append(GITEA_BASE_URL.replace("://localhost", "://gitea"))

    last_error: Exception | None = None
    tried = []
    for base in dict.fromkeys(base_candidates):
        tried.append(base)
        try:
            return _request_to_base(base, method, path, data)
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="ignore")
            raise RuntimeError(f"Gitea API error {exc.code} on {path}: {detail}") from exc
        except Exception as exc:  # noqa: BLE001
            last_error = exc
            continue

    if last_error is None:
        raise RuntimeError(f"Gitea API failed for {path}")
    try:
        raise last_error
    except Exception as exc:  # noqa: BLE001
        raise RuntimeError(f"Gitea API connection failed on {path}; tried {tried}: {exc}") from exc


def run(cmd: list[str], cwd: Path | None = None, timeout: int = 300) -> tuple[int, str]:
    proc = subprocess.run(
        cmd,
        cwd=str(cwd) if cwd else str(ROOT_DIR),
        capture_output=True,
        text=True,
        timeout=timeout,
        env=os.environ.copy(),
    )
    return proc.returncode, (proc.stdout + "\n" + proc.stderr).strip()


def load_state() -> dict[str, Any]:
    if not STATE_PATH.exists():
        return {"issues": {}}
    return json.loads(STATE_PATH.read_text(encoding="utf-8"))


def save_state(state: dict[str, Any]) -> None:
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    STATE_PATH.write_text(json.dumps(state, indent=2) + "\n", encoding="utf-8")


def list_open_issues() -> list[dict[str, Any]]:
    query = urllib.parse.urlencode({"state": "open", "limit": "50"})
    issues = gitea_request("GET", f"/api/v1/repos/{GITEA_OWNER}/{GITEA_REPO}/issues?{query}")
    return [i for i in issues if "pull_request" not in i]


def classify_issue(issue: dict[str, Any]) -> str:
    text = f"{issue.get('title', '')}\n{issue.get('body', '')}".lower()
    bug_hints = ["bug", "error", "broken", "fail", "exception", "crash", "regression", "fix"]
    return "bug" if any(h in text for h in bug_hints) else "feature"


def select_skills(issue: dict[str, Any]) -> list[str]:
    text = f"{issue.get('title', '')}\n{issue.get('body', '')}".lower()
    skills = [
        "docs/skills/agent-guidelines.md",
        "docs/skills/solution-architect.md",
        "docs/skills/developer.md",
        "docs/workflows/ci-cd.md",
    ]
    matrix = {
        "docker": "docs/architecture/ai-fabric-poc.md",
        "runner": "docs/workflows/ci-cd.md",
        "workflow": "docs/workflows/pr-best-practices.md",
        "pr": "docs/workflows/pr-best-practices.md",
        "telegram": "README.md",
        "bot": "README.md",
        "docs": "docs/README.md",
        "issue": "docs/workflows/pr-best-practices.md",
    }
    for key, path in matrix.items():
        if key in text and path not in skills:
            skills.append(path)
    return skills


def has_architect_block(issue: dict[str, Any]) -> bool:
    body = issue.get("body") or ""
    return ARCH_START in body and ARCH_END in body


def issue_branch(issue_number: int, title: str) -> str:
    slug = re.sub(r"[^a-z0-9]+", "-", title.lower()).strip("-")
    slug = slug[:40] if slug else "task"
    return f"issue/{issue_number}-{slug}"


def worktree_path(issue_number: int) -> Path:
    return ROOT_DIR / "var" / "agents" / f"issue-{issue_number}"


def ensure_worktree(branch: str, path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists():
        run(["git", "worktree", "remove", "--force", str(path)])
    run(["git", "fetch", "origin"], cwd=ROOT_DIR)
    code, out = run(["git", "worktree", "add", "-B", branch, str(path), f"origin/{BASE_BRANCH}"], cwd=ROOT_DIR)
    if code != 0:
        raise RuntimeError(f"Failed to prepare worktree: {out}")


def write_prompt(path: Path, issue: dict[str, Any], issue_type: str, skills: list[str], extra: str = "") -> Path:
    prompt_path = path / ".issue-agent-prompt.md"
    skill_lines = "\n".join(f"- `{s}`" for s in skills)
    prompt = (
        f"# Issue #{issue['number']} execution\n\n"
        f"Type: {issue_type}\n"
        f"Title: {issue.get('title', '')}\n\n"
        "## Task\n"
        "Implement the issue end-to-end in this repository:\n"
        "- write or update tests first where applicable\n"
        "- implement minimal safe changes\n"
        "- run `./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh`\n"
        "- commit changes\n"
        "- push branch\n\n"
        "## Issue body\n"
        f"{issue.get('body') or '(empty)'}\n\n"
        "## Relevant skills/docs to read first\n"
        f"{skill_lines}\n\n"
        "## Constraints\n"
        "- Keep changes scoped to this issue\n"
        "- Follow PR template and workflow policies\n"
        "- If CI fails, fix and retry until green within attempt limit\n"
    )
    if extra:
        prompt += f"\n## Additional context\n{extra}\n"
    prompt_path.write_text(prompt, encoding="utf-8")
    return prompt_path


def write_architect_prompt(path: Path, issue: dict[str, Any], issue_type: str, skills: list[str]) -> Path:
    prompt_path = path / ".issue-architect-prompt.md"
    skill_lines = "\n".join(f"- `{s}`" for s in skills)
    prompt = (
        f"# Solution Architect analysis for issue #{issue['number']}\n\n"
        f"Type: {issue_type}\n"
        f"Title: {issue.get('title', '')}\n\n"
        "You are acting as Solution Architect.\n"
        "Produce a concise markdown analysis with this exact structure:\n\n"
        "## Possible Solutions\n"
        "- Option A: ...\n"
        "- Option B: ...\n\n"
        "## Recommended Approach\n"
        "- Why this option is preferred\n"
        "- Risks\n"
        "- Dependencies\n\n"
        "## Estimation\n"
        "- Complexity: (S|M|L)\n"
        "- Estimated effort: <time>\n"
        "- Test scope: <brief>\n\n"
        "## Required Skills/Context\n"
        "- list relevant docs/skills from repository\n\n"
        "Do not include any content outside these sections.\n\n"
        "## Issue body\n"
        f"{issue.get('body') or '(empty)'}\n\n"
        "## Relevant skills/docs\n"
        f"{skill_lines}\n"
    )
    prompt_path.write_text(prompt, encoding="utf-8")
    return prompt_path


def run_agent(path: Path, prompt_file: Path) -> tuple[int, str]:
    if shutil.which(AGENT_BIN) is None:
        return 127, f"Agent binary not found: {AGENT_BIN}"
    cmd = [AGENT_BIN, "run", "--cwd", str(path), "--prompt-file", str(prompt_file)]
    if AGENT_EXTRA_ARGS:
        cmd.extend(shlex.split(AGENT_EXTRA_ARGS))
    return run(cmd, cwd=path, timeout=3600)


def run_checks(path: Path) -> tuple[int, str]:
    return run(["/bin/bash", "-lc", "./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh"], cwd=path, timeout=1200)


def extract_agent_output(text: str) -> str:
    cleaned = text.strip()
    if not cleaned:
        return "(no output)"
    cleaned = cleaned[:ARCHITECT_MAX_CHARS]
    return cleaned


def update_issue_body(issue: dict[str, Any], architect_text: str, issue_type: str, skills: list[str]) -> None:
    issue_number = issue["number"]
    body = issue.get("body") or ""
    context_block = (
        f"{ARCH_START}\n"
        "## Solution Architect\n"
        f"- Type classification: `{issue_type}`\n"
        f"- Suggested skills/context: {', '.join(f'`{s}`' for s in skills)}\n\n"
        f"{architect_text}\n"
        f"{ARCH_END}"
    )

    if ARCH_START in body and ARCH_END in body:
        pattern = re.compile(re.escape(ARCH_START) + r".*?" + re.escape(ARCH_END), re.DOTALL)
        body = pattern.sub(context_block, body)
    else:
        body = (body.strip() + "\n\n" + context_block).strip()

    gitea_request(
        "PATCH",
        f"/api/v1/repos/{GITEA_OWNER}/{GITEA_REPO}/issues/{issue_number}",
        {"body": body},
    )


def commit_and_push(path: Path, branch: str, issue: dict[str, Any], issue_type: str) -> str:
    code, _ = run(["git", "status", "--porcelain"], cwd=path)
    if code != 0:
        raise RuntimeError("Cannot inspect git status in worktree")
    status = subprocess.check_output(["git", "status", "--porcelain"], cwd=path, text=True).strip()
    if not status:
        raise RuntimeError("Agent made no file changes")
    run(["git", "add", "."], cwd=path)
    prefix = "fix" if issue_type == "bug" else "feat"
    msg = f"{prefix}(issue #{issue['number']}): {issue.get('title', '').strip()[:60]}"
    code, out = run(["git", "commit", "-m", msg], cwd=path, timeout=120)
    if code != 0:
        raise RuntimeError(f"Commit failed: {out}")
    code, out = run(["git", "push", "-u", "origin", branch], cwd=path, timeout=300)
    if code != 0:
        raise RuntimeError(f"Push failed: {out}")
    return out


def find_open_pr(branch: str) -> dict[str, Any] | None:
    prs = gitea_request("GET", f"/api/v1/repos/{GITEA_OWNER}/{GITEA_REPO}/pulls?state=open")
    for pr in prs:
        head = ((pr.get("head") or {}).get("ref")) or ""
        if head == branch:
            return pr
    return None


def create_pr(issue: dict[str, Any], branch: str, issue_type: str, skills: list[str]) -> str:
    existing = find_open_pr(branch)
    if existing:
        return existing.get("html_url", "")
    body = (
        f"Closes #{issue['number']}\n\n"
        f"Automated by issue handler.\n\n"
        f"Type: {issue_type}\n"
        "Skills/docs considered:\n"
        + "\n".join(f"- `{s}`" for s in skills)
    )
    payload = {
        "title": f"[agent] {issue.get('title', '')}",
        "head": branch,
        "base": BASE_BRANCH,
        "body": body,
    }
    pr = gitea_request("POST", f"/api/v1/repos/{GITEA_OWNER}/{GITEA_REPO}/pulls", payload)
    return pr.get("html_url", "")


def comment_issue(issue_number: int, body: str) -> None:
    gitea_request("POST", f"/api/v1/repos/{GITEA_OWNER}/{GITEA_REPO}/issues/{issue_number}/comments", {"body": body})


def get_issue(issue_number: int) -> dict[str, Any]:
    return gitea_request("GET", f"/api/v1/repos/{GITEA_OWNER}/{GITEA_REPO}/issues/{issue_number}")


def process_issue(issue: dict[str, Any], state: dict[str, Any]) -> None:
    num = issue["number"]
    title = issue.get("title", "")
    branch = issue_branch(num, title)
    path = worktree_path(num)
    issue_type = classify_issue(issue)
    skills = select_skills(issue)
    issue_state = state.setdefault("issues", {}).setdefault(str(num), {})
    print(f"[issue-handler] processing issue #{num} -> {branch}")

    if DRY_RUN:
        print(f"[issue-handler] dry-run: would process issue #{num}")
        return

    issue_state["status"] = "in_progress"
    issue_state["updated_at"] = int(time.time())
    save_state(state)

    ensure_worktree(branch, path)

    architect_already_done = bool(issue_state.get("architect_done")) or has_architect_block(issue)
    if ARCHITECT_ENABLED and not architect_already_done:
        comment_issue(num, "[issue-handler] Running Solution Architect analysis and updating issue body.")
        architect_prompt = write_architect_prompt(path, issue, issue_type, skills)
        a_code, a_out = run_agent(path, architect_prompt)
        if a_code != 0:
            raise RuntimeError(f"Solution Architect stage failed: {a_out[:800]}")
        architect_md = extract_agent_output(a_out)
        update_issue_body(issue, architect_md, issue_type, skills)
        issue = gitea_request("GET", f"/api/v1/repos/{GITEA_OWNER}/{GITEA_REPO}/issues/{num}")
        issue_state["architect_done"] = True
        issue_state["architect_at"] = int(time.time())
        save_state(state)

    comment_issue(num, f"[issue-handler] Claimed issue. Starting developer implementation on branch `{branch}`.")

    prompt_file = write_prompt(path, issue, issue_type, skills)
    code, out = run_agent(path, prompt_file)
    if code != 0:
        raise RuntimeError(f"Agent execution failed: {out[:800]}")

    attempt = 0
    while attempt < MAX_FIX_ATTEMPTS:
        c_code, c_out = run_checks(path)
        if c_code == 0:
            break
        attempt += 1
        if attempt >= MAX_FIX_ATTEMPTS:
            raise RuntimeError(f"Checks still failing after {MAX_FIX_ATTEMPTS} attempts:\n{c_out[:1200]}")
        fix_prompt = write_prompt(
            path,
            issue,
            issue_type,
            skills,
            extra=f"Checks failed. Fix all failures.\n\n{c_out[:2000]}",
        )
        a_code, a_out = run_agent(path, fix_prompt)
        if a_code != 0:
            raise RuntimeError(f"Agent fix attempt failed: {a_out[:800]}")

    commit_and_push(path, branch, issue, issue_type)
    pr_url = create_pr(issue, branch, issue_type, skills)
    comment_issue(num, f"[issue-handler] Opened PR: {pr_url}")

    issue_state["status"] = "pr_opened"
    issue_state["branch"] = branch
    issue_state["pr_url"] = pr_url
    issue_state["updated_at"] = int(time.time())
    save_state(state)


def run_once(target_issue: int | None = None) -> None:
    state = load_state()
    if target_issue is not None:
        issue = get_issue(target_issue)
        issues = [issue] if issue.get("state") == "open" and "pull_request" not in issue else []
    else:
        issues = list_open_issues()
    for issue in issues:
        num = issue["number"]
        current = state.get("issues", {}).get(str(num), {})
        # Never re-trigger automatically once an issue is tracked.
        # This prevents duplicate architect runs and avoids retriggering on issue edits.
        if target_issue is None and current.get("status") in {"in_progress", "pr_opened", "failed"}:
            continue
        if target_issue is not None and current.get("status") == "pr_opened":
            continue
        try:
            process_issue(issue, state)
        except Exception as exc:  # noqa: BLE001
            msg = f"[issue-handler] Failed on issue #{num}: {exc}"
            print(msg)
            if not DRY_RUN:
                try:
                    comment_issue(num, msg)
                except Exception:
                    pass
            state.setdefault("issues", {})[str(num)] = {
                "status": "failed",
                "error": str(exc),
                "updated_at": int(time.time()),
            }
            save_state(state)


def main() -> int:
    parser = argparse.ArgumentParser(description="Issue handler for autonomous agent execution.")
    parser.add_argument("--once", action="store_true", help="Run a single polling cycle")
    parser.add_argument("--issue-number", type=int, help="Process only a specific issue number")
    args = parser.parse_args()

    if not GITEA_TOKEN:
        raise SystemExit("GITEA_BOT_TOKEN is required in environment")

    if args.once:
        run_once(target_issue=args.issue_number)
        return 0

    while True:
        run_once(target_issue=args.issue_number)
        time.sleep(POLL_INTERVAL)


if __name__ == "__main__":
    raise SystemExit(main())
