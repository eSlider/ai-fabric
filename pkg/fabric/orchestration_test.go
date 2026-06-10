package fabric

import (
	"context"
	"testing"

	"example.org/ai-fabric/internal/config"
)

type mockGiteaClient struct {
	comments []string
}

func (m *mockGiteaClient) ListOpenIssues(ctx context.Context, owner, repo string) ([]map[string]interface{}, error) {
	return nil, nil
}
func (m *mockGiteaClient) GetIssue(ctx context.Context, owner, repo string, number int) (map[string]interface{}, error) {
	return map[string]interface{}{
		"number": float64(number),
		"title":  "Test Issue",
		"state":  "open",
	}, nil
}
func (m *mockGiteaClient) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	m.comments = append(m.comments, body)
	return nil
}
func (m *mockGiteaClient) UpdateIssueState(ctx context.Context, owner, repo string, number int, state string) error {
	return nil
}
func (m *mockGiteaClient) UpdateIssueBody(ctx context.Context, owner, repo string, number int, body string) error {
	return nil
}
func (m *mockGiteaClient) CreatePullRequest(ctx context.Context, owner, repo string, opts map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"html_url": "http://example.com/pr/1"}, nil
}
func (m *mockGiteaClient) ListPullRequests(ctx context.Context, owner, repo string, state string) ([]map[string]interface{}, error) {
	return nil, nil
}

func TestProcessIssueDryRun(t *testing.T) {
	mockGitea := &mockGiteaClient{}
	cfg := &config.Config{}
	cfg.Gitea.Owner = "owner"
	cfg.Gitea.Repo = "repo"
	cfg.Issue.DryRun = true
	cfg.Issue.MaxFixAttempts = 3

	h := &IssueHandler{
		Cfg:         cfg,
		GiteaClient: mockGitea,
	}

	state := make(map[string]interface{})
	issue := map[string]interface{}{
		"number": float64(123),
		"title":  "Test",
	}

	err := h.ProcessIssue(issue, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	issueState := state["issue-123"].(map[string]interface{})
	if issueState["status"] != "dry_run" {
		t.Errorf("expected dry_run, got %v", issueState["status"])
	}
}
