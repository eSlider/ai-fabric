// Package gitea provides a typed client for the Gitea API used by the fabric.
// It reuses the official Gitea SDK types (sdk.Issue, sdk.PullRequest, ...) as
// domain types instead of duplicating them.
package gitea

import (
	"fmt"
	"net/http"
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
	ListOwnerRepos(owner string, limit int) ([]*sdk.Repository, error)
	BaseURL() string
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

func (s *Service) BaseURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.BaseURL
}

func (s *Service) resetSDKClient() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.helper = nil
	s.currentUser = nil
}

func withSDK[T any](s *Service, call func(*sdk.Client) (T, error)) (T, error) {
	var zero T
	cli, err := s.sdkClient()
	if err != nil {
		return zero, err
	}
	result, err := call(cli)
	if err == nil {
		return result, nil
	}
	if fallback, ok := FallbackBaseURLForDNSError(s.cfg.BaseURL, err); ok {
		s.resetSDKClient()
		s.mu.Lock()
		s.cfg.BaseURL = fallback
		s.mu.Unlock()
		cli, err = s.sdkClient()
		if err != nil {
			return zero, err
		}
		return call(cli)
	}
	return zero, err
}

func isNotFoundHTTP(resp *sdk.Response, err error) bool {
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return true
	}
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "404") || strings.Contains(msg, "not found") {
			return true
		}
	}
	return false
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
		if fallback, ok := FallbackBaseURLForDNSError(s.cfg.BaseURL, err); ok {
			s.cfg.BaseURL = fallback
			cli, err = helpers.NewClient(helpers.Config{
				URL:   fallback,
				Token: s.cfg.Token,
				Owner: s.cfg.Owner,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("gitea client init: %w", err)
		}
	}
	s.helper = cli
	return cli.SDK, nil
}

// CurrentUser returns (and caches) the user owning the configured token.
func (s *Service) CurrentUser() (*sdk.User, error) {
	if s.currentUser != nil {
		return s.currentUser, nil
	}
	user, err := withSDK(s, func(cli *sdk.Client) (*sdk.User, error) {
		user, _, err := cli.GetMyUserInfo()
		return user, err
	})
	if err != nil {
		return nil, fmt.Errorf("gitea current user: %w", err)
	}
	s.currentUser = user
	return user, nil
}

// ListOwnerRepos lists repositories for owner, trying org first then user on 404.
func (s *Service) ListOwnerRepos(owner string, limit int) ([]*sdk.Repository, error) {
	if limit <= 0 {
		limit = 20
	}
	repos, err := withSDK(s, func(cli *sdk.Client) ([]*sdk.Repository, error) {
		listOpt := sdk.ListOrgReposOptions{
			ListOptions: sdk.ListOptions{Page: 1, PageSize: limit},
		}
		repos, resp, err := cli.ListOrgRepos(owner, listOpt)
		if err != nil && isNotFoundHTTP(resp, err) {
			userOpt := sdk.ListReposOptions{
				ListOptions: sdk.ListOptions{Page: 1, PageSize: limit},
			}
			repos, _, err = cli.ListUserRepos(owner, userOpt)
		}
		return repos, err
	})
	if err != nil {
		return nil, fmt.Errorf("gitea list repos for %s: %w", owner, err)
	}
	return repos, nil
}

// CreateIssue opens a new issue.
func (s *Service) CreateIssue(owner, repo, title, body string) (*sdk.Issue, error) {
	issue, err := withSDK(s, func(cli *sdk.Client) (*sdk.Issue, error) {
		issue, _, err := cli.CreateIssue(owner, repo, sdk.CreateIssueOption{Title: title, Body: body})
		return issue, err
	})
	if err != nil {
		return nil, fmt.Errorf("gitea create issue %s/%s: %w", owner, repo, err)
	}
	return issue, nil
}

// ListOpenIssues returns all open non-PR issues.
func (s *Service) ListOpenIssues(owner, repo string) ([]*sdk.Issue, error) {
	const pageSize = 50
	var all []*sdk.Issue
	for page := 1; ; page++ {
		pageNum := page
		issues, err := withSDK(s, func(cli *sdk.Client) ([]*sdk.Issue, error) {
			issues, _, err := cli.ListRepoIssues(owner, repo, sdk.ListIssueOption{
				State:       sdk.StateOpen,
				Type:        sdk.IssueTypeIssue,
				ListOptions: sdk.ListOptions{Page: pageNum, PageSize: pageSize},
			})
			return issues, err
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
	issue, err := withSDK(s, func(cli *sdk.Client) (*sdk.Issue, error) {
		issue, _, err := cli.GetIssue(owner, repo, number)
		return issue, err
	})
	if err != nil {
		return nil, fmt.Errorf("gitea get issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return issue, nil
}

func (s *Service) UpdateIssueBody(owner, repo string, number int64, body string) error {
	_, err := withSDK(s, func(cli *sdk.Client) (struct{}, error) {
		_, _, err := cli.EditIssue(owner, repo, number, sdk.EditIssueOption{Body: &body})
		return struct{}{}, err
	})
	if err != nil {
		return fmt.Errorf("gitea edit issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}

func (s *Service) CreateIssueComment(owner, repo string, number int64, body string) (*sdk.Comment, error) {
	comment, err := withSDK(s, func(cli *sdk.Client) (*sdk.Comment, error) {
		comment, _, err := cli.CreateIssueComment(owner, repo, number, sdk.CreateIssueCommentOption{Body: body})
		return comment, err
	})
	if err != nil {
		return nil, fmt.Errorf("gitea comment issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return comment, nil
}

func (s *Service) EditIssueComment(owner, repo string, commentID int64, body string) error {
	_, err := withSDK(s, func(cli *sdk.Client) (struct{}, error) {
		_, _, err := cli.EditIssueComment(owner, repo, commentID, sdk.EditIssueCommentOption{Body: body})
		return struct{}{}, err
	})
	if err != nil {
		return fmt.Errorf("gitea edit comment %s/%s id=%d: %w", owner, repo, commentID, err)
	}
	return nil
}

func (s *Service) ListIssueComments(owner, repo string, number int64) ([]*sdk.Comment, error) {
	comments, err := withSDK(s, func(cli *sdk.Client) ([]*sdk.Comment, error) {
		comments, _, err := cli.ListIssueComments(owner, repo, number, sdk.ListIssueCommentOptions{})
		return comments, err
	})
	if err != nil {
		return nil, fmt.Errorf("gitea list comments %s/%s#%d: %w", owner, repo, number, err)
	}
	return comments, nil
}

func (s *Service) AddIssueLabel(owner, repo string, number int64, label string) error {
	return s.withIssueClient(func(cli *sdk.Client) error {
		id, err := s.labelID(cli, owner, repo, label, true)
		if err != nil {
			return err
		}
		_, _, err = cli.AddIssueLabels(owner, repo, number, sdk.IssueLabelsOption{Labels: []int64{id}})
		if err != nil {
			return fmt.Errorf("gitea add label %q to %s/%s#%d: %w", label, owner, repo, number, err)
		}
		return nil
	})
}

func (s *Service) RemoveIssueLabel(owner, repo string, number int64, label string) error {
	return s.withIssueClient(func(cli *sdk.Client) error {
		id, err := s.labelID(cli, owner, repo, label, false)
		if err != nil || id == 0 {
			return err
		}
		_, err = cli.DeleteIssueLabel(owner, repo, number, id)
		if err != nil {
			return fmt.Errorf("gitea remove label %q from %s/%s#%d: %w", label, owner, repo, number, err)
		}
		return nil
	})
}

func (s *Service) withIssueClient(call func(*sdk.Client) error) error {
	_, err := withSDK(s, func(cli *sdk.Client) (struct{}, error) {
		return struct{}{}, call(cli)
	})
	return err
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
	pr, err := withSDK(s, func(cli *sdk.Client) (*sdk.PullRequest, error) {
		pr, _, err := cli.CreatePullRequest(owner, repo, opt)
		return pr, err
	})
	if err != nil {
		return nil, fmt.Errorf("gitea create PR %s/%s head=%s: %w", owner, repo, opt.Head, err)
	}
	return pr, nil
}

func (s *Service) ListOpenPullRequests(owner, repo string) ([]*sdk.PullRequest, error) {
	prs, err := withSDK(s, func(cli *sdk.Client) ([]*sdk.PullRequest, error) {
		prs, _, err := cli.ListRepoPullRequests(owner, repo, sdk.ListPullRequestsOptions{
			State:       sdk.StateOpen,
			ListOptions: sdk.ListOptions{PageSize: 50},
		})
		return prs, err
	})
	if err != nil {
		return nil, fmt.Errorf("gitea list PRs %s/%s: %w", owner, repo, err)
	}
	return prs, nil
}

func (s *Service) GetPullRequest(owner, repo string, number int64) (*sdk.PullRequest, error) {
	pr, err := withSDK(s, func(cli *sdk.Client) (*sdk.PullRequest, error) {
		pr, _, err := cli.GetPullRequest(owner, repo, number)
		return pr, err
	})
	if err != nil {
		return nil, fmt.Errorf("gitea get PR %s/%s#%d: %w", owner, repo, number, err)
	}
	return pr, nil
}

// MergePullRequest merges an open PR with the default merge style.
func (s *Service) MergePullRequest(owner, repo string, number int64) error {
	merged, err := withSDK(s, func(cli *sdk.Client) (bool, error) {
		merged, _, err := cli.MergePullRequest(owner, repo, number, sdk.MergePullRequestOption{
			Style: sdk.MergeStyleMerge,
		})
		return merged, err
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
	_, err := withSDK(s, func(cli *sdk.Client) (struct{}, error) {
		closed := sdk.StateClosed
		_, _, err := cli.EditIssue(owner, repo, number, sdk.EditIssueOption{State: &closed})
		return struct{}{}, err
	})
	if err != nil {
		return fmt.Errorf("gitea close issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return nil
}
