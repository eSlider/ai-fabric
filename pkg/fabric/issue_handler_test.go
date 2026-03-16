package fabric

import "testing"

func TestClassifyIssue(t *testing.T) {
	h := &IssueHandler{}

	bugIssue := map[string]interface{}{
		"title": "Something is broken",
		"body":  "I got an error when running the script",
	}
	if got := h.ClassifyIssue(bugIssue); got != "bug" {
		t.Errorf("expected bug, got %s", got)
	}

	featIssue := map[string]interface{}{
		"title": "Add new feature",
		"body":  "Please implement this cool thing",
	}
	if got := h.ClassifyIssue(featIssue); got != "feature" {
		t.Errorf("expected feature, got %s", got)
	}
}

func TestSelectSkills(t *testing.T) {
	h := &IssueHandler{}

	issue := map[string]interface{}{
		"title": "Fix docker runner issues",
		"body":  "The telegram bot is failing in CI/CD workflow",
	}

	skills := h.SelectSkills(issue)
	expectedSkills := []string{
		"docs/skills/agent-guidelines.md",
		"docs/skills/solution-architect.md",
		"docs/skills/developer.md",
		"docs/workflows/ci-cd.md",
		"docs/architecture/ai-fabric-poc.md",
		"README.md",
		"docs/workflows/pr-best-practices.md",
	}

	for _, expected := range expectedSkills {
		found := false
		for _, got := range skills {
			if got == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected skill: %s", expected)
		}
	}
}
