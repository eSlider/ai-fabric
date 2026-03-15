#!/usr/bin/env python3
import json
import os
import re
import subprocess
import time
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Optional


ROOT_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
BOT_TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
BOT_NAME = os.environ.get("TELEGRAM_BOT_NAME", "ai-fabric-bot").strip()
ALLOWED_CHAT_IDS_RAW = os.environ.get("TELEGRAM_ALLOWED_CHAT_IDS", "").strip()
ALLOWED_USERNAMES_RAW = os.environ.get("TELEGRAM_ALLOWED_USERNAMES", "").strip()
GITEA_BASE_URL = os.environ.get("GITEA_BOT_BASE_URL", "http://localhost:3000").strip().rstrip("/")
GITEA_OWNER = os.environ.get("GITEA_BOT_OWNER", "eslider").strip()
GITEA_REPO = os.environ.get("GITEA_BOT_REPO", "ai-fabric").strip()
GITEA_TOKEN = os.environ.get("GITEA_BOT_TOKEN", "").strip()
ISSUE_HANDLER_TRIGGER_ON_CREATE = os.environ.get("ISSUE_HANDLER_TRIGGER_ON_CREATE", "1").strip() == "1"
API_BASE = f"https://api.telegram.org/bot{BOT_TOKEN}"

ALLOWED_CHAT_IDS = {
    int(x.strip()) for x in ALLOWED_CHAT_IDS_RAW.split(",") if x.strip().lstrip("-").isdigit()
}
ALLOWED_USERNAMES = {x.strip().lower() for x in ALLOWED_USERNAMES_RAW.split(",") if x.strip()}
TASK_DRAFTS: dict[int, "TaskDraft"] = {}


@dataclass
class TaskDraft:
    task_type: str
    raw: str
    fields: dict[str, str] = field(default_factory=dict)


def tg_post(method: str, data: dict) -> dict:
    payload = urllib.parse.urlencode(data).encode("utf-8")
    req = urllib.request.Request(
        f"{API_BASE}/{method}",
        data=payload,
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )
    with urllib.request.urlopen(req, timeout=40) as resp:
        return json.loads(resp.read().decode("utf-8"))


def send_message(chat_id: int, text: str) -> None:
    tg_post(
        "sendMessage",
        {
            "chat_id": str(chat_id),
            "text": text[:4000],
            "disable_web_page_preview": "true",
        },
    )


def gitea_request(method: str, path: str, data: Optional[dict] = None) -> dict:
    if not GITEA_TOKEN:
        raise RuntimeError("GITEA_BOT_TOKEN is not configured")
    body = None
    headers = {"Authorization": f"token {GITEA_TOKEN}", "Content-Type": "application/json"}
    if data is not None:
        body = json.dumps(data).encode("utf-8")
    req = urllib.request.Request(f"{GITEA_BASE_URL}{path}", data=body, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=40) as resp:
        return json.loads(resp.read().decode("utf-8"))


def classify_task(text: str) -> str:
    bug_hints = [
        "bug",
        "error",
        "broken",
        "fail",
        "failed",
        "not work",
        "issue",
        "fix",
        "incorrect",
        "regression",
    ]
    t = text.lower()
    return "bug" if any(h in t for h in bug_hints) else "feature"


def compact_title(text: str, task_type: str) -> str:
    cleaned = re.sub(r"\s+", " ", text).strip()
    if not cleaned:
        cleaned = "Task from Telegram"
    cleaned = cleaned[:90]
    prefix = "[bug]" if task_type == "bug" else "[feature]"
    return f"{prefix} {cleaned}"


def missing_fields(task: TaskDraft) -> list[str]:
    if task.task_type == "bug":
        required = ["problem", "steps", "expected", "actual"]
    else:
        required = ["goal", "value", "acceptance"]
    return [f for f in required if not task.fields.get(f)]


def question_for(field_name: str, task_type: str) -> str:
    if task_type == "bug":
        mapping = {
            "problem": "What is broken? Describe the bug in one clear sentence.",
            "steps": "How to reproduce it? Provide exact steps.",
            "expected": "What should happen (expected behavior)?",
            "actual": "What happens now (actual behavior)?",
        }
    else:
        mapping = {
            "goal": "What should be built or changed?",
            "value": "Why is it needed? What user or business value does it provide?",
            "acceptance": "Provide acceptance criteria (checklist or clear done conditions).",
        }
    return mapping[field_name]


def parse_field_answer(text: str, field_name: str) -> str:
    return re.sub(r"\s+", " ", text).strip()


def build_issue_body(task: TaskDraft) -> str:
    if task.task_type == "bug":
        return (
            "## Type\nbug\n\n"
            f"## Problem\n{task.fields['problem']}\n\n"
            f"## Steps To Reproduce\n{task.fields['steps']}\n\n"
            f"## Expected Behavior\n{task.fields['expected']}\n\n"
            f"## Actual Behavior\n{task.fields['actual']}\n\n"
            f"## Original Request\n{task.raw}\n"
        )
    return (
        "## Type\nfeature\n\n"
        f"## Goal\n{task.fields['goal']}\n\n"
        f"## Value\n{task.fields['value']}\n\n"
        f"## Acceptance Criteria\n{task.fields['acceptance']}\n\n"
        f"## Original Request\n{task.raw}\n"
    )


def trigger_issue_handler(issue_number: int) -> None:
    if not ISSUE_HANDLER_TRIGGER_ON_CREATE:
        return
    # Fire-and-forget single-issue architect/developer pipeline.
    subprocess.Popen(
        ["/bin/bash", "-lc", f"./bin/issue_handler.sh --once --issue-number {issue_number}"],
        cwd=ROOT_DIR,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env=os.environ.copy(),
    )


def create_gitea_issue(task: TaskDraft) -> tuple[str, int]:
    payload = {
        "title": compact_title(task.raw, task.task_type),
        "body": build_issue_body(task),
    }
    issue = gitea_request("POST", f"/api/v1/repos/{GITEA_OWNER}/{GITEA_REPO}/issues", payload)
    url = issue.get("html_url", "")
    number = int(issue.get("number", 0) or 0)
    if number > 0:
        trigger_issue_handler(number)
    return url, number


def start_task_flow(chat_id: int, text: str) -> str:
    task_type = classify_task(text)
    draft = TaskDraft(task_type=task_type, raw=text)
    TASK_DRAFTS[chat_id] = draft
    missing = missing_fields(draft)
    first = missing[0]
    return (
        f"I classified this as: {task_type}.\n"
        "I need a bit more detail before creating a Gitea issue.\n\n"
        f"{question_for(first, task_type)}"
    )


def continue_task_flow(chat_id: int, text: str) -> str:
    draft = TASK_DRAFTS[chat_id]
    needed = missing_fields(draft)
    if not needed:
        url, number = create_gitea_issue(draft)
        TASK_DRAFTS.pop(chat_id, None)
        if url and number > 0:
            return f"Issue created: {url}\nSolution Architect pipeline triggered for issue #{number}."
        return f"Issue created: {url}" if url else "Issue created."

    current = needed[0]
    draft.fields[current] = parse_field_answer(text, current)
    needed = missing_fields(draft)
    if needed:
        return question_for(needed[0], draft.task_type)

    url, number = create_gitea_issue(draft)
    TASK_DRAFTS.pop(chat_id, None)
    if url and number > 0:
        return f"Issue created: {url}\nSolution Architect pipeline triggered for issue #{number}."
    return f"Issue created: {url}" if url else "Issue created."


def run_cmd(cmd: list[str], timeout: int = 45) -> tuple[int, str]:
    try:
        proc = subprocess.run(
            cmd,
            cwd=ROOT_DIR,
            capture_output=True,
            text=True,
            timeout=timeout,
            env=os.environ.copy(),
        )
        out = (proc.stdout + "\n" + proc.stderr).strip()
        return proc.returncode, out
    except subprocess.TimeoutExpired:
        return 124, "Command timed out."


def ensure_allowed(chat_id: int, username: Optional[str]) -> bool:
    username_norm = (username or "").strip().lower()
    if ALLOWED_USERNAMES and username_norm not in ALLOWED_USERNAMES:
        return False
    if ALLOWED_CHAT_IDS and chat_id not in ALLOWED_CHAT_IDS:
        return False
    if ALLOWED_USERNAMES or ALLOWED_CHAT_IDS:
        return True
    return True


def format_result(code: int, output: str) -> str:
    status = "OK" if code == 0 else f"ERR({code})"
    snippet = output[:3200] if output else "(no output)"
    return f"{status}\n\n{snippet}"


def handle_command(chat_id: int, text: str) -> None:
    cmd = text.strip()
    if cmd.startswith("/start") or cmd.startswith("/help"):
        send_message(
            chat_id,
            (
                f"{BOT_NAME} commands:\n"
                "/status - docker compose status\n"
                "/health - run healthcheck script\n"
                "/up - start stack\n"
                "/down - stop stack\n"
                "/checks - run fmt/lint/test/review policy\n"
                "/task <description> - classify and create a clear Gitea issue\n"
                "/logs <service> - tail logs for gitea|gitea-runner-1|gitea-runner-2|issue-handler|telegram-bot"
            ),
        )
        return

    if cmd.startswith("/task"):
        raw = cmd[len("/task") :].strip()
        if not raw:
            send_message(chat_id, "Usage: /task <description>")
            return
        send_message(chat_id, start_task_flow(chat_id, raw))
        return

    if cmd == "/status":
        code, out = run_cmd(["docker", "compose", "-f", "docker-compose.yml", "ps"])
        send_message(chat_id, format_result(code, out))
        return

    if cmd == "/health":
        code, out = run_cmd(["./bin/healthcheck.sh"])
        send_message(chat_id, format_result(code, out))
        return

    if cmd == "/up":
        code, out = run_cmd(["./bin/up.sh"], timeout=120)
        send_message(chat_id, format_result(code, out))
        return

    if cmd == "/down":
        code, out = run_cmd(["./bin/down.sh"], timeout=120)
        send_message(chat_id, format_result(code, out))
        return

    if cmd == "/checks":
        code, out = run_cmd(
            [
                "/bin/bash",
                "-lc",
                "./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh && ./bin/review_policy.sh",
            ],
            timeout=120,
        )
        send_message(chat_id, format_result(code, out))
        return

    if cmd.startswith("/logs"):
        parts = cmd.split(maxsplit=1)
        if len(parts) != 2:
            send_message(chat_id, "Usage: /logs <service>")
            return
        svc = parts[1].strip()
        allowed = {"gitea", "gitea-runner-1", "gitea-runner-2", "issue-handler", "telegram-bot"}
        if svc not in allowed:
            send_message(chat_id, "Allowed services: gitea, gitea-runner-1, gitea-runner-2, issue-handler, telegram-bot")
            return
        code, out = run_cmd(
            ["docker", "compose", "-f", "docker-compose.yml", "logs", "--tail=60", svc],
            timeout=60,
        )
        send_message(chat_id, format_result(code, out))
        return

    send_message(chat_id, "Unknown command. Use /help.")


def extract_message(update: dict) -> tuple[Optional[int], Optional[str], Optional[str]]:
    msg = update.get("message") or {}
    chat = msg.get("chat") or {}
    from_user = msg.get("from") or {}
    text = msg.get("text")
    chat_id = chat.get("id")
    username = from_user.get("username")
    if isinstance(chat_id, int) and isinstance(text, str):
        return chat_id, text, username if isinstance(username, str) else None
    return None, None, None


def main() -> int:
    if not BOT_TOKEN:
        raise SystemExit("TELEGRAM_BOT_TOKEN is required")

    print(f"[bot] starting polling as {BOT_NAME}")
    offset = 0
    while True:
        try:
            resp = tg_post(
                "getUpdates",
                {"timeout": "25", "offset": str(offset), "allowed_updates": '["message"]'},
            )
            for item in resp.get("result", []):
                update_id = int(item["update_id"])
                offset = update_id + 1
                chat_id, text, username = extract_message(item)
                if chat_id is None or text is None:
                    continue
                if not ensure_allowed(chat_id, username):
                    send_message(chat_id, "Access denied.")
                    continue
                if text.startswith("/"):
                    handle_command(chat_id, text)
                    continue
                if chat_id in TASK_DRAFTS:
                    try:
                        send_message(chat_id, continue_task_flow(chat_id, text))
                    except Exception as exc:  # noqa: BLE001
                        TASK_DRAFTS.pop(chat_id, None)
                        send_message(chat_id, f"Could not create issue: {exc}")
                    continue
                send_message(chat_id, "Unknown command. Use /help.")
        except Exception as exc:  # noqa: BLE001
            print(f"[bot] polling error: {exc}")
            time.sleep(3)


if __name__ == "__main__":
    raise SystemExit(main())
