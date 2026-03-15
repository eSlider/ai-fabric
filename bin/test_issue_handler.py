import unittest
from pathlib import Path

import issue_handler


class TriggerPathValidationTests(unittest.TestCase):
    def test_valid_single_issue_trigger_path_has_no_errors(self) -> None:
        errors = issue_handler.collect_trigger_path_errors(
            once=True,
            issue_number=5,
            trigger_event_hint="single_issue",
            trigger_repo_hint=f"{issue_handler.GITEA_OWNER}/{issue_handler.GITEA_REPO}",
            trigger_branch_hint=issue_handler.BASE_BRANCH,
            trigger_script_hint=str((issue_handler.ROOT_DIR / "bin" / "issue_handler.sh").resolve()),
            expected_script_path=issue_handler.ROOT_DIR / "bin" / "issue_handler.sh",
            owner=issue_handler.GITEA_OWNER,
            repo=issue_handler.GITEA_REPO,
            base_branch=issue_handler.BASE_BRANCH,
        )
        self.assertEqual(errors, [])

    def test_single_issue_requires_once_mode(self) -> None:
        errors = issue_handler.collect_trigger_path_errors(
            once=False,
            issue_number=5,
            trigger_event_hint="single_issue",
            trigger_repo_hint=None,
            trigger_branch_hint=None,
            trigger_script_hint=None,
            expected_script_path=issue_handler.ROOT_DIR / "bin" / "issue_handler.sh",
            owner=issue_handler.GITEA_OWNER,
            repo=issue_handler.GITEA_REPO,
            base_branch=issue_handler.BASE_BRANCH,
        )
        self.assertTrue(any("requires --once" in err for err in errors))

    def test_event_mismatch_is_reported(self) -> None:
        errors = issue_handler.collect_trigger_path_errors(
            once=True,
            issue_number=12,
            trigger_event_hint="poll_open_issues",
            trigger_repo_hint=None,
            trigger_branch_hint=None,
            trigger_script_hint=None,
            expected_script_path=issue_handler.ROOT_DIR / "bin" / "issue_handler.sh",
            owner=issue_handler.GITEA_OWNER,
            repo=issue_handler.GITEA_REPO,
            base_branch=issue_handler.BASE_BRANCH,
        )
        self.assertTrue(any("Trigger event mismatch" in err for err in errors))

    def test_script_mismatch_is_reported(self) -> None:
        wrong_script = str((issue_handler.ROOT_DIR / "bin" / "telegram_bot.sh").resolve())
        errors = issue_handler.collect_trigger_path_errors(
            once=True,
            issue_number=9,
            trigger_event_hint="single_issue",
            trigger_repo_hint=None,
            trigger_branch_hint=None,
            trigger_script_hint=wrong_script,
            expected_script_path=issue_handler.ROOT_DIR / "bin" / "issue_handler.sh",
            owner=issue_handler.GITEA_OWNER,
            repo=issue_handler.GITEA_REPO,
            base_branch=issue_handler.BASE_BRANCH,
        )
        self.assertTrue(any("Trigger script mismatch" in err for err in errors))

    def test_invalid_repo_slug_is_reported(self) -> None:
        errors = issue_handler.collect_trigger_path_errors(
            once=True,
            issue_number=9,
            trigger_event_hint="single_issue",
            trigger_repo_hint=None,
            trigger_branch_hint=None,
            trigger_script_hint=None,
            expected_script_path=Path("/tmp/issue_handler.sh"),
            owner="",
            repo="",
            base_branch="main",
        )
        self.assertTrue(any("Invalid repository slug" in err for err in errors))


if __name__ == "__main__":
    unittest.main()
