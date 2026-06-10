// Package gitea provides a typed client for the Gitea API used by the fabric.
// It reuses the official Gitea SDK types (sdk.Issue, sdk.PullRequest, ...) as
// domain types instead of duplicating them.
package gitea

import (
	"fmt"
	"strings"
	"sync"

	sdk "code.gitea.io/sdk/gitea"
	helpers "github.com/eslider/go-gitea-helpers"
)

// Client exposes the Gitea operations used by the fabric, with SDK types as the boundary.
type Client interface {
	CurrentUser() (*sdk.User, error)
	CreateIssue(owner, repo, title, body string) (*sdk.Issue, error)
	ListOpenIssues(owner, repo string) ([]*sdk.Issue, error)
	GetIssue(owner, repo string, number int64) (*sdk.Issue, error)
	UpdateIssueBody(owner, repo string, number int64, body string) error
	CreateIssueComment(owner, repo string, number int64, body string) (*sdk.Comment, error)
	EditIssueComment(owner, repo string, commentID int64, body string) error
	ListIssueComments(owner, repo string, number int64) ([]*sdk.Comment, error)
	AddIssueLabel(owner, repo string, number int64, label string) error
	RemoveIssueLabel(owner, repo string, number int64, label string) error
	CreatePullRequest(owner, repo string, opt sdk.CreatePullRequestOption) (*sdk.PullRequest, error)
	ListOpenPullRequests(owner, repo string) ([]*sdk.PullRequest, error)
	GetPullRequest(owner, repo string, number int64) (*sdk.PullRequest, error)
	MergePullRequest(owner, repo string, number int64) error
	CloseIssue(owner, repo string, number int64) error
}

// Service implements Client on top of the official Gitea SDK.
type Service struct {
	cfg BotConfig

	mu          sync.Mutex
	helper      *helpers.Client
	currentUser *sdk.User
	labelIDs    map[string]int64 // "owner/repo/label" -> label ID
}

func NewService(cfg BotConfig) *Service {
	return &Service{cfg: cfg, labelIDs: map[string]int64{}}
}

func (s *Service) sdkClient() (*sdk.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.helper != nil {
		return s.helper.SDK, nil
	}
	cli, err := helpers.NewClient(helpers.Config{
		URL:   s.cfg.BaseURL,
		Token: s.cfg.Token,
		Owner: s.cfg.Owner,
	})
	if err != nil {
		return nil, fmt.Errorf("gitea client init: %w", err)
	}
	s.helper = cli
	return cli.SDK, nil
}

// CurrentUser returns (and caches) the user owning the configured token.
func (s *Service) CurrentUser() (*sdk.User, error) {
	if s.currentUser != nil {
		return s.currentUser, nil
	}
	cli, err := s.sdkClient()
	if err != nil {
		return nil, err
	}
	user, _, err := cli.GetMyUserInfo()
	if err != nil {
		return nil, fmt.Errorf("gitea current user: %w", err)
	}
	s.currentUser = user
	return user, nil
}

// CreateIssue opens a new issue.
func (s *Service) CreateIssue(owner, repo, title, body string) (*sdk.Issue, error) {
	cli, err := s.sdkClient()
	if err != nil {
		return nil, err
	}
	issue, _, err := cli.CreateIssue(owner, repo, sdk.CreateIssueOption{Title: title, Body: body})
	if err != nil {
		return nil, fmt.Errorf("gitea create issue %s/%s: %w", owner, repo, err)
	}
	return issue, nil
}

// ListOpenIssues returns all open non-PR issues.
func (s *Service) ListOpenIssues(owner, repo string) ([]*sdk.Issue, error) {
	cli, err := s.sdkClient()
	if err != nil {
		return nil, err
	}
	const pageSize = 50
	var all []*sdk.Issue
	for page := 1; ; page++ {
		issues, _, err := cli.ListRepoIssues(owner, repo, sdk.ListIssueOption{
			State:       sdk.StateOpen,
			Type:        sdk.IssueTypeIssue,
			ListOptions: sdk.ListOptions{Page: page, PageSize: pageSize},
		})
		if err != nil {
			return nil, fmt.Errorf("gitea list issues %s/%s page %d: %w", owner, repo, page, err)
		}
		if len(issues) == 0 {
			return all, nil
		}
		all = append(all, issues...)
	}
}

func (s *Service) GetIssue(owner, repo string, number int64) (*sdk.Issue, error) {
	cli, err := s.sdkClient()
	if err != nil {
		return nil, err
	}
	issue, _, err := cli.GetIssue(owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("gitea get issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return issue, nil
}

func (s *Service) UpdateIssueBody(owner, repo string, number int64, body string) error {
	cli, err := s.sdkClient()
	if err != nil {
		return err
	}
	_, _, err = cli.EditIssue(owner, repo, number, sdk.EditIssueOption{Body: &body})
	if err != nil {
		return fmt.Errorf("gitea edit issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

func (s *Service) CreateIssueComment(owner, repo string, number int64, body string) (*sdk.Comment, error) {
	cli, err := s.sdkClient()
	if err != nil {
		return nil, err
	}
	comment, _, err := cli.CreateIssueComment(owner, repo, number, sdk.CreateIssueCommentOption{Body: body})
	if err != nil {
		return nil, fmt.Errorf("gitea comment issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return comment, nil
}

func (s *Service) EditIssueComment(owner, repo string, commentID int64, body string) error {
	cli, err := s.sdkClient()
	if err != nil {
		return err
	}
	_, _, err = cli.EditIssueComment(owner, repo, commentID, sdk.EditIssueCommentOption{Body: body})
	if err != nil {
		return fmt.Errorf("gitea edit comment %s/%s id=%d: %w", owner, repo, commentID, err)
	}
	return nil
}

func (s *Service) ListIssueComments(owner, repo string, number int64) ([]*sdk.Comment, error) {
	cli, err := s.sdkClient()
	if err != nil {
		return nil, err
	}
	comments, _, err := cli.ListIssueComments(owner, repo, number, sdk.ListIssueCommentOptions{})
	if err != nil {
		return nil, fmt.Errorf("gitea list comments %s/%s#%d: %w", owner, repo, number, err)
	}
	return comments, nil
}

func (s *Service) AddIssueLabel(owner, repo string, number int64, label string) error {
	cli, err := s.sdkClient()
	if err != nil {
		return err
	}
	id, err := s.labelID(cli, owner, repo, label, true)
	if err != nil {
		return err
	}
	_, _, err = cli.AddIssueLabels(owner, repo, number, sdk.IssueLabelsOption{Labels: []int64{id}})
	if err != nil {
		return fmt.Errorf("gitea add label %q to %s/%s#%d: %w", label, owner, repo, number, err)
	}
	return nil
}

func (s *Service) RemoveIssueLabel(owner, repo string, number int64, label string) error {
	cli, err := s.sdkClient()
	if err != nil {
		return err
	}
	id, err := s.labelID(cli, owner, repo, label, false)
	if err != nil || id == 0 {
		return err
	}
	_, err = cli.DeleteIssueLabel(owner, repo, number, id)
	if err != nil {
		return fmt.Errorf("gitea remove label %q from %s/%s#%d: %w", label, owner, repo, number, err)
	}
	return nil
}

// labelID resolves a repo label name to its ID, optionally creating the label.
// Returns 0 without error when the label does not exist and create is false.
func (s *Service) labelID(cli *sdk.Client, owner, repo, label string, create bool) (int64, error) {
	key := owner + "/" + repo + "/" + label
	s.mu.Lock()
	if id, ok := s.labelIDs[key]; ok {
		s.mu.Unlock()
		return id, nil
	}
	s.mu.Unlock()

	labels, _, err := cli.ListRepoLabels(owner, repo, sdk.ListLabelsOptions{ListOptions: sdk.ListOptions{PageSize: 100}})
	if err != nil {
		return 0, fmt.Errorf("gitea list labels %s/%s: %w", owner, repo, err)
	}
	for _, l := range labels {
		s.mu.Lock()
		s.labelIDs[owner+"/"+repo+"/"+l.Name] = l.ID
		s.mu.Unlock()
		if l.Name == label {
			return l.ID, nil
		}
	}
	if !create {
		return 0, nil
	}
	created, _, err := cli.CreateLabel(owner, repo, sdk.CreateLabelOption{Name: label, Color: labelColor(label)})
	if err != nil {
		return 0, fmt.Errorf("gitea create label %q in %s/%s: %w", label, owner, repo, err)
	}
	s.mu.Lock()
	s.labelIDs[key] = created.ID
	s.mu.Unlock()
	return created.ID, nil
}

func labelColor(label string) string {
	switch {
	case strings.Contains(label, "failed"), strings.Contains(label, "needs_human"):
		return "#cc0000"
	case strings.Contains(label, "completed"), strings.Contains(label, "pr_opened"):
		return "#00aa00"
	default:
		return "#0066cc"
	}
}

func (s *Service) CreatePullRequest(owner, repo string, opt sdk.CreatePullRequestOption) (*sdk.PullRequest, error) {
	cli, err := s.sdkClient()
	if err != nil {
		return nil, err
	}
	pr, _, err := cli.CreatePullRequest(owner, repo, opt)
	if err != nil {
		return nil, fmt.Errorf("gitea create PR %s/%s head=%s: %w", owner, repo, opt.Head, err)
	}
	return pr, nil
}

func (s *Service) ListOpenPullRequests(owner, repo string) ([]*sdk.PullRequest, error) {
	cli, err := s.sdkClient()
	if err != nil {
		return nil, err
	}
	prs, _, err := cli.ListRepoPullRequests(owner, repo, sdk.ListPullRequestsOptions{
		State:       sdk.StateOpen,
		ListOptions: sdk.ListOptions{PageSize: 50},
	})
	if err != nil {
		return nil, fmt.Errorf("gitea list PRs %s/%s: %w", owner, repo, err)
	}
	return prs, nil
}

func (s *Service) GetPullRequest(owner, repo string, number int64) (*sdk.PullRequest, error) {
	cli, err := s.sdkClient()
	if err != nil {
		return nil, err
	}
	pr, _, err := cli.GetPullRequest(owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("gitea get PR %s/%s#%d: %w", owner, repo, number, err)
	}
	return pr, nil
}

// MergePullRequest merges an open PR with the default merge style.
func (s *Service) MergePullRequest(owner, repo string, number int64) error {
	cli, err := s.sdkClient()
	if err != nil {
		return err
	}
	merged, _, err := cli.MergePullRequest(owner, repo, number, sdk.MergePullRequestOption{
		Style: sdk.MergeStyleMerge,
	})
	if err != nil {
		return fmt.Errorf("gitea merge PR %s/%s#%d: %w", owner, repo, number, err)
	}
	if !merged {
		return fmt.Errorf("gitea merge PR %s/%s#%d: not merged", owner, repo, number)
	}
	return nil
}

// CloseIssue closes an issue.
func (s *Service) CloseIssue(owner, repo string, number int64) error {
	cli, err := s.sdkClient()
	if err != nil {
		return err
	}
	closed := sdk.StateClosed
	_, _, err = cli.EditIssue(owner, repo, number, sdk.EditIssueOption{State: &closed})
	if err != nil {
		return fmt.Errorf("gitea close issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}
