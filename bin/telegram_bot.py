#!/usr/bin/env python3
import json
import os
import subprocess
import time
import urllib.parse
import urllib.request
from typing import Optional


ROOT_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
BOT_TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
BOT_NAME = os.environ.get("TELEGRAM_BOT_NAME", "ai-fabric-bot").strip()
ALLOWED_CHAT_IDS_RAW = os.environ.get("TELEGRAM_ALLOWED_CHAT_IDS", "").strip()
ALLOWED_USERNAMES_RAW = os.environ.get("TELEGRAM_ALLOWED_USERNAMES", "").strip()
API_BASE = f"https://api.telegram.org/bot{BOT_TOKEN}"

ALLOWED_CHAT_IDS = {
    int(x.strip()) for x in ALLOWED_CHAT_IDS_RAW.split(",") if x.strip().lstrip("-").isdigit()
}
ALLOWED_USERNAMES = {x.strip().lower() for x in ALLOWED_USERNAMES_RAW.split(",") if x.strip()}


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
                "/logs <service> - tail logs for gitea|gitea-runner|postgres|telegram-bot"
            ),
        )
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
        allowed = {"gitea", "gitea-runner", "postgres", "telegram-bot"}
        if svc not in allowed:
            send_message(chat_id, "Allowed services: gitea, gitea-runner, postgres, telegram-bot")
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
                handle_command(chat_id, text)
        except Exception as exc:  # noqa: BLE001
            print(f"[bot] polling error: {exc}")
            time.sleep(3)


if __name__ == "__main__":
    raise SystemExit(main())
