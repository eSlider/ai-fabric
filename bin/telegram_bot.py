#!/usr/bin/env python3
import json
import os
import re
import subprocess
import time
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
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
ISSUE_APPROVALS_FILE = os.environ.get("ISSUE_APPROVALS_FILE", "var/issue-handler/approvals.json").strip()
API_BASE = f"https://api.telegram.org/bot{BOT_TOKEN}"

ALLOWED_CHAT_IDS = {
    int(x.strip()) for x in ALLOWED_CHAT_IDS_RAW.split(",") if x.strip().lstrip("-").isdigit()
}
ALLOWED_USERNAMES = {x.strip().lower() for x in ALLOWED_USERNAMES_RAW.split(",") if x.strip()}
TASK_DRAFTS: dict[int, "TaskDraft"] = {}
SELECTED_PROJECTS: dict[int, str] = {}
PROJECT_CREATION_PENDING: dict[int, bool] = {}


@dataclass
class TaskDraft:
    task_type: str
    raw: str
    chat_id: int
    owner: str
    repo: str
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


def send_message(chat_id: int, text: str, reply_markup: Optional[dict] = None) -> None:
    payload = {
        "chat_id": str(chat_id),
        "text": text[:4000],
        "disable_web_page_preview": "true",
    }
    if reply_markup is not None:
        payload["reply_markup"] = json.dumps(reply_markup)
    tg_post("sendMessage", payload)


def answer_callback_query(callback_query_id: str, text: str = "") -> None:
    payload = {"callback_query_id": callback_query_id}
    if text:
        payload["text"] = text[:180]
    tg_post("answerCallbackQuery", payload)


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
    marker = f"<!-- ai-fabric:telegram-chat-id:{task.chat_id} -->\n"
    if task.task_type == "bug":
        return (
            marker
            +
            "## Type\nbug\n\n"
            f"## Problem\n{task.fields['problem']}\n\n"
            f"## Steps To Reproduce\n{task.fields['steps']}\n\n"
            f"## Expected Behavior\n{task.fields['expected']}\n\n"
            f"## Actual Behavior\n{task.fields['actual']}\n\n"
            f"## Original Request\n{task.raw}\n"
        )
    return (
        marker
        +
        "## Type\nfeature\n\n"
        f"## Goal\n{task.fields['goal']}\n\n"
        f"## Value\n{task.fields['value']}\n\n"
        f"## Acceptance Criteria\n{task.fields['acceptance']}\n\n"
        f"## Original Request\n{task.raw}\n"
    )


def trigger_issue_handler(issue_number: int, owner: str, repo: str) -> None:
    if not ISSUE_HANDLER_TRIGGER_ON_CREATE:
        return
    # Fire-and-forget single-issue architect/developer pipeline.
    env = os.environ.copy()
    env["ISSUE_TRIGGER_EVENT"] = "single_issue"
    env["ISSUE_TRIGGER_REPO"] = f"{owner}/{repo}"
    env["ISSUE_TRIGGER_BASE_BRANCH"] = os.environ.get("ISSUE_BASE_BRANCH", "main")
    env["ISSUE_HANDLER_TRIGGER_SCRIPT"] = str((Path(ROOT_DIR) / "bin" / "issue_handler.sh").resolve())
    subprocess.Popen(
        ["/bin/bash", "-lc", f"./bin/issue_handler.sh --once --issue-number {issue_number}"],
        cwd=ROOT_DIR,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env=env,
    )


def create_gitea_issue(task: TaskDraft) -> tuple[str, int]:
    payload = {
        "title": compact_title(task.raw, task.task_type),
        "body": build_issue_body(task),
    }
    issue = gitea_request("POST", f"/api/v1/repos/{task.owner}/{task.repo}/issues", payload)
    url = issue.get("html_url", "")
    number = int(issue.get("number", 0) or 0)
    if number > 0:
        trigger_issue_handler(number, task.owner, task.repo)
    return url, number


def start_task_flow(chat_id: int, text: str) -> str:
    task_type = classify_task(text)
    owner, repo = selected_project_slug(chat_id)
    draft = TaskDraft(task_type=task_type, raw=text, chat_id=chat_id, owner=owner, repo=repo)
    TASK_DRAFTS[chat_id] = draft
    missing = missing_fields(draft)
    first = missing[0]
    return (
        f"I classified this as: {task_type}.\n"
        f"Project: {owner}/{repo}\n"
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


def approvals_path() -> Path:
    p = Path(ISSUE_APPROVALS_FILE)
    return p if p.is_absolute() else Path(ROOT_DIR) / p


def load_approvals() -> dict:
    p = approvals_path()
    if not p.exists():
        return {"issues": {}}
    try:
        return json.loads(p.read_text(encoding="utf-8"))
    except Exception:
        return {"issues": {}}


def save_approvals(data: dict) -> None:
    p = approvals_path()
    p.parent.mkdir(parents=True, exist_ok=True)
    tmp = p.with_suffix(".tmp")
    tmp.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
    tmp.replace(p)


def pending_approval_for_chat(chat_id: int) -> Optional[tuple[str, dict]]:
    data = load_approvals()
    issues = data.get("issues") or {}
    pending = []
    for issue_num, rec in issues.items():
        if int(rec.get("chat_id", 0) or 0) != chat_id:
            continue
        if (rec.get("status") or "").lower() != "pending":
            continue
        pending.append((issue_num, rec))
    if not pending:
        return None
    pending.sort(key=lambda x: int(x[1].get("updated_at", 0) or 0), reverse=True)
    return pending[0]


def handle_approval_reply(chat_id: int, text: str) -> bool:
    pending = pending_approval_for_chat(chat_id)
    if pending is None:
        return False
    issue_num, rec = pending
    t = text.strip().lower()
    options = rec.get("options") or []

    if t in {"cancel", "no", "/cancel"}:
        rec["status"] = "cancelled"
        rec["decision"] = "cancelled"
        rec["updated_at"] = int(time.time())
        data = load_approvals()
        data.setdefault("issues", {})[issue_num] = rec
        save_approvals(data)
        send_message(chat_id, f"Cancelled issue #{issue_num}. It will be closed by handler.")
        return True

    if t in {"confirm", "approve", "yes", "ok", "/approve"}:
        rec["status"] = "approved"
        rec["decision"] = "recommended"
        rec["selected_option_index"] = None
        rec["selected_option_text"] = ""
        rec["updated_at"] = int(time.time())
        data = load_approvals()
        data.setdefault("issues", {})[issue_num] = rec
        save_approvals(data)
        send_message(chat_id, f"Approved recommended approach for issue #{issue_num}.")
        return True

    if t.isdigit():
        idx = int(t) - 1
        if idx < 0 or idx >= len(options):
            send_message(chat_id, f"Invalid option. Choose 1..{len(options)}, or use confirm/cancel.")
            return True
        rec["status"] = "approved"
        rec["decision"] = "selected_option"
        rec["selected_option_index"] = idx + 1
        rec["selected_option_text"] = options[idx]
        rec["updated_at"] = int(time.time())
        data = load_approvals()
        data.setdefault("issues", {})[issue_num] = rec
        save_approvals(data)
        send_message(chat_id, f"Approved option {idx+1} for issue #{issue_num}.")
        return True

    send_message(chat_id, "Approval pending. Reply with option number, confirm, or cancel.")
    return True


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


def selected_project_ref(chat_id: int) -> str:
    return SELECTED_PROJECTS.get(chat_id, f"{GITEA_OWNER}/{GITEA_REPO}")


def selected_project_slug(chat_id: int) -> tuple[str, str]:
    project = selected_project_ref(chat_id)
    if "/" not in project:
        return GITEA_OWNER, GITEA_REPO
    owner, repo = project.split("/", 1)
    owner_clean = owner.strip() or GITEA_OWNER
    repo_clean = repo.strip() or GITEA_REPO
    return owner_clean, repo_clean


def list_user_projects() -> list[str]:
    repos = gitea_request("GET", "/api/v1/user/repos?limit=100")
    projects: list[str] = []
    if isinstance(repos, list):
        for item in repos:
            if not isinstance(item, dict):
                continue
            full_name = item.get("full_name")
            if isinstance(full_name, str) and full_name.strip():
                projects.append(full_name.strip())
    current_default = f"{GITEA_OWNER}/{GITEA_REPO}"
    if current_default not in projects:
        projects.append(current_default)
    return sorted(set(projects))


def build_projects_keyboard(chat_id: int, projects: list[str]) -> dict:
    current = selected_project_ref(chat_id)
    keyboard: list[list[dict[str, str]]] = []
    for project in projects:
        prefix = "✅ " if project == current else ""
        keyboard.append([{"text": f"{prefix}{project}", "callback_data": f"select|{project}"}])
    keyboard.append(
        [
            {"text": "Create new project", "callback_data": "create"},
            {"text": "Refresh", "callback_data": "refresh"},
        ]
    )
    return {"inline_keyboard": keyboard}


def projects_menu_text(chat_id: int, projects: list[str]) -> str:
    current = selected_project_ref(chat_id)
    return (
        "Select project for /task.\n"
        f"Current project: {current}\n\n"
        f"Available projects: {len(projects)}"
    )


def show_projects_menu(chat_id: int) -> None:
    projects = list_user_projects()
    send_message(
        chat_id,
        projects_menu_text(chat_id, projects),
        reply_markup=build_projects_keyboard(chat_id, projects),
    )


def normalize_project_name(raw: str) -> str:
    base = re.sub(r"\s+", "-", raw.strip().lower())
    cleaned = re.sub(r"[^a-z0-9._-]", "-", base)
    compact = re.sub(r"-{2,}", "-", cleaned).strip("-._")
    return compact[:80]


def create_user_project(name: str) -> str:
    created = gitea_request("POST", "/api/v1/user/repos", {"name": name})
    if isinstance(created, dict):
        full_name = created.get("full_name")
        if isinstance(full_name, str) and full_name.strip():
            return full_name.strip()
        owner = created.get("owner")
        if isinstance(owner, dict):
            login = owner.get("login")
            repo_name = created.get("name")
            if isinstance(login, str) and login and isinstance(repo_name, str) and repo_name:
                return f"{login}/{repo_name}"
    return f"{GITEA_OWNER}/{name}"


def handle_project_callback(chat_id: int, data: str) -> None:
    if data.startswith("select|"):
        project = data[len("select|") :].strip()
        if project:
            SELECTED_PROJECTS[chat_id] = project
            PROJECT_CREATION_PENDING.pop(chat_id, None)
            send_message(chat_id, f"Current project set to {project}")
            return
        send_message(chat_id, "Could not select project.")
        return
    if data == "create":
        PROJECT_CREATION_PENDING[chat_id] = True
        send_message(chat_id, "Send new project name in next message.")
        return
    if data == "refresh":
        show_projects_menu(chat_id)
        return
    send_message(chat_id, "Unknown projects action.")


def handle_non_command_message(chat_id: int, text: str) -> None:
    if PROJECT_CREATION_PENDING.get(chat_id):
        normalized = normalize_project_name(text)
        if not normalized:
            send_message(chat_id, "Invalid project name. Use letters, numbers, dots, dashes or underscores.")
            return
        created = create_user_project(normalized)
        SELECTED_PROJECTS[chat_id] = created
        PROJECT_CREATION_PENDING.pop(chat_id, None)
        send_message(chat_id, f"Project created and selected: {created}")
        show_projects_menu(chat_id)
        return
    if chat_id in TASK_DRAFTS:
        send_message(chat_id, continue_task_flow(chat_id, text))
        return
    send_message(chat_id, "Unknown command. Use /help.")


def route_non_command_message(chat_id: int, text: str) -> None:
    # Keep conversational task/project flows deterministic. Approval replies should
    # not hijack text while user is already providing required task details.
    if PROJECT_CREATION_PENDING.get(chat_id) or chat_id in TASK_DRAFTS:
        handle_non_command_message(chat_id, text)
        return
    if handle_approval_reply(chat_id, text):
        return
    handle_non_command_message(chat_id, text)


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
                "/projects - choose or create project for /task\n"
                "/task <description> - classify and create a clear Gitea issue\n"
                "When Solution Architect asks for approval, reply: <option-number> | confirm | cancel\n"
                "/logs <service> - tail logs for gitea|gitea-runner-1|gitea-runner-2|issue-handler|telegram-bot"
            ),
        )
        return

    if cmd == "/projects":
        try:
            show_projects_menu(chat_id)
        except Exception as exc:  # noqa: BLE001
            send_message(chat_id, f"Could not load projects: {exc}")
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


def extract_callback_query(update: dict) -> tuple[Optional[str], Optional[int], Optional[str], Optional[str]]:
    callback = update.get("callback_query") or {}
    callback_id = callback.get("id")
    from_user = callback.get("from") or {}
    username = from_user.get("username")
    data = callback.get("data")
    message = callback.get("message") or {}
    chat = message.get("chat") or {}
    chat_id = chat.get("id")
    if isinstance(callback_id, str) and isinstance(chat_id, int) and isinstance(data, str):
        user = username if isinstance(username, str) else None
        return callback_id, chat_id, data, user
    return None, None, None, None


def main() -> int:
    if not BOT_TOKEN:
        raise SystemExit("TELEGRAM_BOT_TOKEN is required")

    print(f"[bot] starting polling as {BOT_NAME}")
    offset = 0
    while True:
        try:
            resp = tg_post(
                "getUpdates",
                {"timeout": "25", "offset": str(offset), "allowed_updates": '["message","callback_query"]'},
            )
            for item in resp.get("result", []):
                update_id = int(item["update_id"])
                offset = update_id + 1
                callback_id, callback_chat_id, callback_data, callback_username = extract_callback_query(item)
                if callback_id is not None and callback_chat_id is not None and callback_data is not None:
                    if not ensure_allowed(callback_chat_id, callback_username):
                        answer_callback_query(callback_id, "Access denied")
                        send_message(callback_chat_id, "Access denied.")
                        continue
                    try:
                        handle_project_callback(callback_chat_id, callback_data)
                        answer_callback_query(callback_id)
                    except Exception as exc:  # noqa: BLE001
                        answer_callback_query(callback_id, "Action failed")
                        send_message(callback_chat_id, f"Could not process projects action: {exc}")
                    continue
                chat_id, text, username = extract_message(item)
                if chat_id is None or text is None:
                    continue
                if not ensure_allowed(chat_id, username):
                    send_message(chat_id, "Access denied.")
                    continue
                if text.startswith("/"):
                    handle_command(chat_id, text)
                    continue
                try:
                    route_non_command_message(chat_id, text)
                except Exception as exc:  # noqa: BLE001
                    TASK_DRAFTS.pop(chat_id, None)
                    PROJECT_CREATION_PENDING.pop(chat_id, None)
                    send_message(chat_id, f"Could not process message: {exc}")
        except Exception as exc:  # noqa: BLE001
            print(f"[bot] polling error: {exc}")
            time.sleep(3)


if __name__ == "__main__":
    raise SystemExit(main())
