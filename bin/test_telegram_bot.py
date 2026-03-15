import unittest
from unittest import mock

import telegram_bot


class CreateGiteaIssueLabelTests(unittest.TestCase):
    def setUp(self) -> None:
        telegram_bot.LABEL_ID_CACHE.clear()

    def test_resolve_issue_label_ids_matches_name_case_insensitive(self) -> None:
        with mock.patch(
            "telegram_bot.gitea_request",
            return_value=[
                {"id": 2, "name": "feature"},
                {"id": 5, "name": "Bug"},
            ],
        ):
            labels = telegram_bot.resolve_issue_label_ids("bug")

        self.assertEqual(labels, [5])

    def test_create_issue_sets_bug_label_id_in_payload(self) -> None:
        task = telegram_bot.TaskDraft(
            task_type="bug",
            raw="Fix login error",
            fields={
                "problem": "Login fails",
                "steps": "Open login page and submit valid credentials",
                "expected": "User is signed in",
                "actual": "A 500 error is shown",
            },
        )

        with (
            mock.patch("telegram_bot.trigger_issue_handler"),
            mock.patch("telegram_bot.resolve_issue_label_ids", return_value=[11]),
            mock.patch(
                "telegram_bot.gitea_request",
                return_value={"html_url": "https://example.test/issues/42", "number": 42},
            ) as gitea_mock,
        ):
            telegram_bot.create_gitea_issue(task)

        _, _, payload = gitea_mock.call_args.args
        self.assertEqual(payload.get("labels"), [11])

    def test_create_issue_omits_labels_when_label_not_found(self) -> None:
        task = telegram_bot.TaskDraft(
            task_type="feature",
            raw="Add dark mode toggle",
            fields={
                "goal": "Add dark mode toggle",
                "value": "Improve usability at night",
                "acceptance": "User can switch theme and preference persists",
            },
        )

        with (
            mock.patch("telegram_bot.trigger_issue_handler"),
            mock.patch("telegram_bot.resolve_issue_label_ids", return_value=[]),
            mock.patch(
                "telegram_bot.gitea_request",
                return_value={"html_url": "https://example.test/issues/7", "number": 7},
            ) as gitea_mock,
        ):
            telegram_bot.create_gitea_issue(task)

        _, _, payload = gitea_mock.call_args.args
        self.assertNotIn("labels", payload)


if __name__ == "__main__":
    unittest.main()
