import unittest

import semantic_version


class SemanticVersionTests(unittest.TestCase):
    def test_latest_semver_tag_picks_highest_valid_tag(self) -> None:
        tag = semantic_version.latest_semver_tag(
            ["foo", "v1.2.3", "v1.10.0", "v1.2.10", "v2.0.0-rc1"]
        )
        self.assertEqual(tag, "v1.10.0")

    def test_required_bump_detects_major_from_breaking_change_footer(self) -> None:
        commits = [
            "feat(api): add endpoint\n\nBREAKING CHANGE: response schema changed",
            "fix: typo",
        ]
        self.assertEqual(semantic_version.required_bump(commits), "major")

    def test_required_bump_detects_major_from_bang_commit(self) -> None:
        commits = ["feat!: remove deprecated command"]
        self.assertEqual(semantic_version.required_bump(commits), "major")

    def test_required_bump_detects_minor_from_feature_commit(self) -> None:
        commits = ["fix: patch", "feat(ui): add release badge"]
        self.assertEqual(semantic_version.required_bump(commits), "minor")

    def test_required_bump_defaults_to_patch_for_regular_commits(self) -> None:
        commits = ["docs: update readme", "chore: cleanup"]
        self.assertEqual(semantic_version.required_bump(commits), "patch")

    def test_bump_version_updates_expected_part(self) -> None:
        self.assertEqual(semantic_version.bump_version("1.2.3", "patch"), "1.2.4")
        self.assertEqual(semantic_version.bump_version("1.2.3", "minor"), "1.3.0")
        self.assertEqual(semantic_version.bump_version("1.2.3", "major"), "2.0.0")

    def test_compute_next_version_uses_latest_tag_and_commits(self) -> None:
        version = semantic_version.compute_next_version(
            latest_tag="v1.4.9",
            commits=["fix: one", "feat: two"],
        )
        self.assertEqual(version, "1.5.0")


if __name__ == "__main__":
    unittest.main()
