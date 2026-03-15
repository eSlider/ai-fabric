import os
import time
import unittest
from unittest.mock import patch

import telegram_bot


def smoke_env_ready() -> bool:
    if os.environ.get("TELEGRAM_TASK_SMOKE", "0").strip() != "1":
        return False
    required = (
        "GITEA_BOT_BASE_URL",
        "GITEA_BOT_OWNER",
        "GITEA_BOT_REPO",
        "GITEA_BOT_TOKEN",
    )
    return all(os.environ.get(name, "").strip() for name in required)


@unittest.skipUnless(
    smoke_env_ready(),
    "Set TELEGRAM_TASK_SMOKE=1 and GITEA_BOT_* vars to run Telegram /task API smoke.",
)
class TelegramTaskApiSmokeTests(unittest.TestCase):
    @patch("telegram_bot.trigger_issue_handler")
    def test_create_issue_via_api_path(self, trigger_issue_handler_mock) -> None:
        marker = str(int(time.time() * 1000))
        owner = telegram_bot.GITEA_OWNER
        repo = telegram_bot.GITEA_REPO

        draft = telegram_bot.TaskDraft(
            task_type="feature",
            raw=f"smoke create issue {marker}",
            chat_id=123456,
            owner=owner,
            repo=repo,
            fields={
                "goal": f"Smoke goal {marker}",
                "value": "CI confidence",
                "acceptance": f"- created marker {marker}",
            },
        )

        url, number = telegram_bot.create_gitea_issue(draft)
        self.assertTrue(url)
        self.assertGreater(number, 0)

        issue = telegram_bot.gitea_request("GET", f"/api/v1/repos/{owner}/{repo}/issues/{number}")
        self.assertEqual(issue.get("number"), number)
        self.assertIn(marker, issue.get("title", ""))
        self.assertIn(marker, issue.get("body", ""))

        telegram_bot.gitea_request("PATCH", f"/api/v1/repos/{owner}/{repo}/issues/{number}", {"state": "closed"})
        trigger_issue_handler_mock.assert_called_once_with(number, owner, repo)


if __name__ == "__main__":
    unittest.main()
