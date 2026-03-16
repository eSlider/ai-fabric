package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	sdk "code.gitea.io/sdk/gitea"
	helpers "github.com/eslider/go-gitea-helpers"
)

const (
	defaultHTTPTimeout   = 40 * time.Second
	defaultCommandTimout = 60 * time.Second
)

// Client exposes Gitea operations used by issue-handler.
// It intentionally uses map-based payloads to keep integration with existing handler code minimal.
type Client interface {
	ListOpenIssues(ctx context.Context, owner, repo string) ([]map[string]interface{}, error)
	GetIssue(ctx context.Context, owner, repo string, number int) (map[string]interface{}, error)
	CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error
	UpdateIssueState(ctx context.Context, owner, repo string, number int, state string) error
}

type Service struct {
	cfg          Config
	rootDir      string
	teaConfigDir string

	helperClient  *helpers.Client
	teaLoginReady bool
}

func NewService(cfg Config, rootDir, teaConfigDir string) *Service {
	cfg.Normalize()
	return &Service{
		cfg:          cfg,
		rootDir:      rootDir,
		teaConfigDir: teaConfigDir,
	}
}

func (s *Service) ListOpenIssues(ctx context.Context, owner, repo string) ([]map[string]interface{}, error) {
	primary := strings.ToLower(strings.TrimSpace(s.cfg.PrimaryTransport))
	switch primary {
	case "cli":
		issues, err := s.listOpenIssuesCLI(ctx, owner, repo)
		if err == nil || !s.cfg.CLIFallbackEnabled {
			return issues, err
		}
		return s.listOpenIssuesSDK(ctx, owner, repo)
	default:
		issues, err := s.listOpenIssuesSDK(ctx, owner, repo)
		if err == nil || !s.cfg.CLIFallbackEnabled {
			return issues, err
		}
		return s.listOpenIssuesCLI(ctx, owner, repo)
	}
}

func (s *Service) GetIssue(ctx context.Context, owner, repo string, number int) (map[string]interface{}, error) {
	primary := strings.ToLower(strings.TrimSpace(s.cfg.PrimaryTransport))
	switch primary {
	case "cli":
		issue, err := s.getIssueCLI(ctx, owner, repo, number)
		if err == nil || !s.cfg.CLIFallbackEnabled {
			return issue, err
		}
		return s.getIssueSDK(ctx, owner, repo, number)
	default:
		issue, err := s.getIssueSDK(ctx, owner, repo, number)
		if err == nil || !s.cfg.CLIFallbackEnabled {
			return issue, err
		}
		return s.getIssueCLI(ctx, owner, repo, number)
	}
}

func (s *Service) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, number)
	payload := map[string]interface{}{"body": body}
	_, err := s.request(ctx, http.MethodPost, path, payload)
	return err
}

func (s *Service) UpdateIssueState(ctx context.Context, owner, repo string, number int, state string) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d", owner, repo, number)
	payload := map[string]interface{}{"state": state}
	_, err := s.request(ctx, http.MethodPatch, path, payload)
	return err
}

func (s *Service) listOpenIssuesSDK(ctx context.Context, owner, repo string) ([]map[string]interface{}, error) {
	cli, err := s.ensureSDKClient()
	if err != nil {
		return nil, err
	}

	const pageSize = 50
	all := make([]map[string]interface{}, 0, pageSize)
	for page := 1; ; page++ {
		issues, _, err := cli.SDK.ListRepoIssues(owner, repo, sdk.ListIssueOption{
			State: sdk.StateOpen,
			ListOptions: sdk.ListOptions{
				Page:     page,
				PageSize: pageSize,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("gitea sdk list issues %s/%s page %d: %w", owner, repo, page, err)
		}
		if len(issues) == 0 {
			break
		}
		for _, issue := range issues {
			item, err := marshalToMap(issue)
			if err != nil {
				return nil, err
			}
			if item["pull_request"] == nil {
				all = append(all, item)
			}
		}
	}
	return all, nil
}

func (s *Service) getIssueSDK(ctx context.Context, owner, repo string, number int) (map[string]interface{}, error) {
	cli, err := s.ensureSDKClient()
	if err != nil {
		return nil, err
	}

	issue, _, err := cli.SDK.GetIssue(owner, repo, int64(number))
	if err != nil {
		return nil, fmt.Errorf("gitea sdk get issue %s/%s#%d: %w", owner, repo, number, err)
	}
	return marshalToMap(issue)
}

func (s *Service) listOpenIssuesCLI(ctx context.Context, owner, repo string) ([]map[string]interface{}, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues?state=open&limit=50", owner, repo)
	res, err := s.cliRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	items, ok := res.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected issue list response type: %T", res)
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		issue, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if issue["pull_request"] == nil {
			filtered = append(filtered, issue)
		}
	}
	return filtered, nil
}

func (s *Service) getIssueCLI(ctx context.Context, owner, repo string, number int) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d", owner, repo, number)
	res, err := s.cliRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	issue, ok := res.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected issue response type: %T", res)
	}
	return issue, nil
}

func (s *Service) ensureSDKClient() (*helpers.Client, error) {
	if s.helperClient != nil {
		return s.helperClient, nil
	}

	cfg := helpers.Config{
		URL:   s.cfg.BaseURL,
		Token: s.cfg.Token,
		Owner: s.cfg.Owner,
	}
	cli, err := helpers.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	s.helperClient = cli
	return s.helperClient, nil
}

func (s *Service) request(ctx context.Context, method, path string, data interface{}) (interface{}, error) {
	primary := strings.ToLower(strings.TrimSpace(s.cfg.PrimaryTransport))
	switch primary {
	case "cli":
		out, err := s.cliRequest(ctx, method, path, data)
		if err == nil || !s.cfg.CLIFallbackEnabled {
			return out, err
		}
		return s.httpRequest(ctx, method, path, data)
	default:
		out, err := s.httpRequest(ctx, method, path, data)
		if err == nil || !s.cfg.CLIFallbackEnabled {
			return out, err
		}
		return s.cliRequest(ctx, method, path, data)
	}
}

func (s *Service) cliRequest(ctx context.Context, method, path string, data interface{}) (interface{}, error) {
	if err := s.ensureTeaLogin(ctx); err != nil {
		return nil, err
	}

	args := []string{"api", "-l", s.cfg.CLI.Login, "-X", strings.ToUpper(method), path}
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		args = append(args, "-d", string(jsonData))
	}

	code, out, err := s.runTea(ctx, args, defaultCommandTimout)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("gitea cli api request failed: method=%s path=%s output=%s", method, path, out)
	}
	if strings.TrimSpace(out) == "" {
		return map[string]interface{}{}, nil
	}

	var result interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return nil, fmt.Errorf("gitea cli returned non-json output for %s: %s", path, out)
	}
	return result, nil
}

func (s *Service) httpRequest(ctx context.Context, method, path string, data interface{}) (interface{}, error) {
	if strings.TrimSpace(s.cfg.Token) == "" {
		return nil, fmt.Errorf("gitea token is required")
	}
	fullURL := strings.TrimRight(s.cfg.BaseURL, "/") + path

	var bodyReader io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+s.cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: defaultHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitea http request failed %d on %s: %s", resp.StatusCode, path, string(body))
	}

	var out interface{}
	err = json.NewDecoder(resp.Body).Decode(&out)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

func (s *Service) ensureTeaLogin(ctx context.Context) error {
	if s.teaLoginReady {
		return nil
	}

	if strings.TrimSpace(s.cfg.CLI.URL) == "" {
		return fmt.Errorf("gitea cli url is required")
	}
	if strings.TrimSpace(s.cfg.CLI.Token) == "" {
		return fmt.Errorf("gitea cli token is required")
	}

	if err := os.MkdirAll(s.teaConfigDir, 0755); err != nil {
		return err
	}

	code, out, _ := s.runTea(ctx, []string{"login", "list"}, 10*time.Second)
	if code == 0 && strings.Contains(out, s.cfg.CLI.Login) {
		s.teaLoginReady = true
		return nil
	}

	loginArgs := []string{
		"login", "add",
		"--name", s.cfg.CLI.Login,
		"--url", s.cfg.CLI.URL,
		"--token", s.cfg.CLI.Token,
	}
	code, out, err := s.runTea(ctx, loginArgs, 20*time.Second)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("tea login add failed: %s", out)
	}

	s.teaLoginReady = true
	return nil
}

func (s *Service) runTea(parent context.Context, args []string, timeout time.Duration) (int, string, error) {
	teaBin := "tea"
	if s.cfg.CLI.Bin != "" {
		teaBin = s.cfg.CLI.Bin
	}

	return runCommand(parent, s.rootDir, s.teaConfigDir, timeout, teaBin, args...)
}

func runCommand(parent context.Context, cwd, teaConfigDir string, timeout time.Duration, binary string, args ...string) (int, string, error) {
	if timeout <= 0 {
		timeout = defaultCommandTimout
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = cwd

	env := os.Environ()
	if teaConfigDir != "" {
		env = append(env, "TEA_CONFIG_DIR="+teaConfigDir)
	}
	cmd.Env = env

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), out, nil
		}
		return -1, out, err
	}
	return 0, out, nil
}

func marshalToMap(v interface{}) (map[string]interface{}, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil && err != io.EOF {
		return nil, err
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}
