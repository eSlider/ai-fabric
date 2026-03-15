import unittest
from unittest.mock import patch

import telegram_bot


class TelegramProjectsFlowTests(unittest.TestCase):
    def setUp(self) -> None:
        telegram_bot.TASK_DRAFTS.clear()
        telegram_bot.SELECTED_PROJECTS.clear()
        telegram_bot.PROJECT_CREATION_PENDING.clear()

    @patch("telegram_bot.gitea_request")
    @patch("telegram_bot.send_message")
    def test_projects_command_lists_repos_and_highlights_current(self, send_message_mock, gitea_request_mock) -> None:
        chat_id = 100
        telegram_bot.SELECTED_PROJECTS[chat_id] = "alice/current-repo"
        gitea_request_mock.return_value = [
            {"full_name": "alice/current-repo"},
            {"full_name": "alice/other-repo"},
        ]

        telegram_bot.handle_command(chat_id, "/projects")

        self.assertEqual(gitea_request_mock.call_count, 1)
        method, path = gitea_request_mock.call_args[0]
        self.assertEqual(method, "GET")
        self.assertTrue(path.startswith("/api/v1/user/repos"))

        args, kwargs = send_message_mock.call_args
        self.assertIn("Select project", args[1])
        self.assertIn("alice/current-repo", args[1])
        keyboard = kwargs["reply_markup"]["inline_keyboard"]
        current_button_text = keyboard[0][0]["text"]
        self.assertTrue(current_button_text.startswith("✅ "))

    @patch("telegram_bot.send_message")
    def test_select_project_callback_updates_selected_project(self, send_message_mock) -> None:
        chat_id = 101

        telegram_bot.handle_project_callback(chat_id, "select|alice/new-repo")

        self.assertEqual(telegram_bot.SELECTED_PROJECTS[chat_id], "alice/new-repo")
        args, _ = send_message_mock.call_args
        self.assertIn("Current project set to alice/new-repo", args[1])

    @patch("telegram_bot.trigger_issue_handler")
    @patch("telegram_bot.gitea_request")
    @patch("telegram_bot.send_message")
    def test_task_flow_creates_issue_in_selected_project(
        self,
        send_message_mock,
        gitea_request_mock,
        trigger_issue_handler_mock,
    ) -> None:
        chat_id = 102
        telegram_bot.SELECTED_PROJECTS[chat_id] = "bob/roadmap"

        def fake_gitea_request(method: str, path: str, data=None):  # noqa: ANN001
            if method == "POST" and path == "/api/v1/repos/bob/roadmap/issues":
                return {"html_url": "https://example.test/i/1", "number": 1}
            raise AssertionError(f"Unexpected request: {method} {path}")

        gitea_request_mock.side_effect = fake_gitea_request

        telegram_bot.handle_command(chat_id, "/task add dashboard widgets")
        telegram_bot.handle_non_command_message(chat_id, "Build widgets dashboard")
        telegram_bot.handle_non_command_message(chat_id, "Lets team track KPIs")
        telegram_bot.handle_non_command_message(chat_id, "Users can add/remove widgets")

        self.assertEqual(gitea_request_mock.call_count, 1)
        self.assertEqual(send_message_mock.call_count, 4)
        args, _ = send_message_mock.call_args
        self.assertIn("Issue created", args[1])
        trigger_issue_handler_mock.assert_called_once_with(1, "bob", "roadmap")

    @patch("telegram_bot.gitea_request")
    @patch("telegram_bot.send_message")
    def test_create_project_flow_creates_and_selects_new_project(self, send_message_mock, gitea_request_mock) -> None:
        chat_id = 103

        def fake_gitea_request(method: str, path: str, data=None):  # noqa: ANN001
            if method == "POST" and path == "/api/v1/user/repos":
                self.assertEqual(data, {"name": "new-project"})
                return {"full_name": "alice/new-project"}
            if method == "GET" and path.startswith("/api/v1/user/repos"):
                return [{"full_name": "alice/new-project"}]
            raise AssertionError(f"Unexpected request: {method} {path}")

        gitea_request_mock.side_effect = fake_gitea_request

        telegram_bot.handle_project_callback(chat_id, "create")
        telegram_bot.handle_non_command_message(chat_id, "New Project")

        self.assertEqual(telegram_bot.SELECTED_PROJECTS[chat_id], "alice/new-project")
        self.assertNotIn(chat_id, telegram_bot.PROJECT_CREATION_PENDING)
        self.assertEqual(send_message_mock.call_count, 3)


if __name__ == "__main__":
    unittest.main()
