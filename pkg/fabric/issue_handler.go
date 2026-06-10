package fabric

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "code.gitea.io/sdk/gitea"

	"example.org/ai-fabric/internal/config"
	"example.org/ai-fabric/pkg/gitea"
)

const (
	ArchStart   = "<!-- ai-fabric:solution-architect:start -->"
	ArchEnd     = "<!-- ai-fabric:solution-architect:end -->"
	TgChatRegex = `<!--\s*ai-fabric:telegram-chat-id:(-?\d+)\s*-->`

	stageArchitect = "architect"
	stageDeveloper = "developer"
	stagePROpened  = "pr_opened"
	stageCompleted = "completed"
	stageFailed    = "failed"
	stageEscalated = "escalated"
)

var closesIssueRegex = regexp.MustCompile(`Closes #(\d+)`)

type IssueHandler struct {
	Cfg *config.Config

	// Per-role clients: each acts as its own Gitea user.
	Developer gitea.Client
	Reviewer  gitea.Client
	Architect gitea.Client

	busyMu     sync.Mutex
	busyIssues map[int64]bool
	busyPRs    map[int64]bool

	modelOnce sync.Once
	model     string

	botLoginsOnce sync.Once
	botLogins     map[string]bool
}

func NewIssueHandler(cfg *config.Config) *IssueHandler {
	roleCfg := func(token string) gitea.BotConfig {
		c := cfg.Gitea.BotConfig
		if token != "" {
			c.Token = token
		}
		return c
	}

	return &IssueHandler{
		Cfg:        cfg,
		Developer:  gitea.NewService(roleCfg(cfg.Gitea.HandlerToken)),
		Reviewer:   gitea.NewService(roleCfg(cfg.Gitea.ReviewerToken)),
		Architect:  gitea.NewService(roleCfg(cfg.Gitea.ArchitectToken)),
		busyIssues: map[int64]bool{},
		busyPRs:    map[int64]bool{},
	}
}

// tryClaim marks an issue or PR as being processed by this instance.
// It is the in-process guard against overlapping poll cycles and webhooks.
func (h *IssueHandler) tryClaim(busy map[int64]bool, number int64) bool {
	h.busyMu.Lock()
	defer h.busyMu.Unlock()
	if busy[number] {
		return false
	}
	busy[number] = true
	return true
}

func (h *IssueHandler) release(busy map[int64]bool, number int64) {
	h.busyMu.Lock()
	defer h.busyMu.Unlock()
	delete(busy, number)
}

// isBotLogin reports whether the login belongs to one of the fabric's own users.
func (h *IssueHandler) isBotLogin(login string) bool {
	h.botLoginsOnce.Do(func() {
		h.botLogins = map[string]bool{}
		for _, client := range []gitea.Client{h.Developer, h.Reviewer, h.Architect} {
			if user, err := client.CurrentUser(); err == nil {
				h.botLogins[user.UserName] = true
			}
		}
	})
	return h.botLogins[login]
}

// workModel resolves the model for agent runs once: explicit override, otherwise
// the cheapest non-fast composer model from `agent --list-models`.
func (h *IssueHandler) workModel() string {
	h.modelOnce.Do(func() {
		h.model = h.detectWorkModel()
		fmt.Printf("[issue-handler] Using agent model: %s\n", h.model)
	})
	return h.model
}

func (h *IssueHandler) detectWorkModel() string {
	if h.Cfg.Issue.SmartModel != "" {
		return h.Cfg.Issue.SmartModel
	}
	out, err := h.runCommand(".", nil, h.Cfg.Issue.AgentBin, "--list-models")
	if err != nil {
		return "auto"
	}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(strings.ToLower(line), "fast") {
			continue
		}
		// Lines look like: "composer-2.5 - Composer 2.5 (current)"; the model id is the first field.
		if id := strings.Fields(line)[0]; strings.HasPrefix(id, "composer") {
			return id
		}
	}
	return "auto"
}

func (h *IssueHandler) TelegramSend(chatID int64, text string) error {
	if h.Cfg.Issue.TelegramBotToken == "" {
		return nil
	}
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", h.Cfg.Issue.TelegramBotToken)
	data := url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"text":    {text},
	}
	resp, err := http.PostForm(apiURL, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (h *IssueHandler) ClassifyIssue(issue *sdk.Issue) string {
	text := strings.ToLower(issue.Title + "\n" + issue.Body)
	bugHints := []string{"bug", "error", "broken", "fail", "exception", "crash", "regression", "fix"}
	for _, hint := range bugHints {
		if strings.Contains(text, hint) {
			return "bug"
		}
	}
	return "feature"
}

func (h *IssueHandler) SelectSkills(issue *sdk.Issue) []string {
	text := strings.ToLower(issue.Title + "\n" + issue.Body)
	skills := []string{
		"docs/skills/agent-guidelines.md",
		"docs/skills/engineering-principles.md",
		"docs/skills/solution-architect.md",
		"docs/skills/developer.md",
		"docs/workflows/ci-cd.md",
	}
	matrix := map[string]string{
		"docker":   "docs/architecture/ai-fabric-poc.md",
		"runner":   "docs/workflows/ci-cd.md",
		"workflow": "docs/workflows/pr-best-practices.md",
		"pr":       "docs/workflows/pr-best-practices.md",
		"telegram": "README.md",
		"bot":      "README.md",
		"docs":     "docs/README.md",
		"issue":    "docs/workflows/pr-best-practices.md",
	}
	for key, path := range matrix {
		if !strings.Contains(text, key) {
			continue
		}
		found := slices.Contains(skills, path)
		if !found {
			skills = append(skills, path)
		}
	}
	return skills
}

func (h *IssueHandler) RunOnce(targetIssue int) {
	fmt.Printf("[issue-handler] Starting cycle... targetIssue=%d\n", targetIssue)

	owner, repo := h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo

	var issues []*sdk.Issue
	if targetIssue > 0 {
		issue, err := h.Developer.GetIssue(owner, repo, int64(targetIssue))
		if err != nil {
			fmt.Printf("[issue-handler] Failed to get issue #%d: %v\n", targetIssue, err)
			return
		}
		if issue.State == sdk.StateOpen && issue.PullRequest == nil {
			issues = append(issues, issue)
		}
	} else {
		var err error
		issues, err = h.Developer.ListOpenIssues(owner, repo)
		if err != nil {
			fmt.Printf("[issue-handler] Failed to list issues: %v\n", err)
			return
		}
	}

	for _, issue := range issues {
		if err := h.ProcessIssue(issue); err != nil {
			fmt.Printf("[issue-handler] Error processing issue #%d: %v\n", issue.Index, err)
		}
	}

	h.syncConflictedPRs()
	h.cleanupWorktrees()
}

// syncConflictedPRs heals open PRs that became unmergeable after the base
// branch moved; Gitea sends no webhook for that, so the poller covers it.
func (h *IssueHandler) syncConflictedPRs() {
	owner, repo := h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo
	prs, err := h.Developer.ListOpenPullRequests(owner, repo)
	if err != nil {
		return
	}
	for _, pr := range prs {
		if !pr.Mergeable {
			h.SyncPullRequest(owner, repo, pr.Index)
		}
	}
}

// worktreeMaxAge is how long an agent worktree may stay untouched before it is
// pruned; worktrees are disposable and recreated on demand.
const worktreeMaxAge = 24 * time.Hour

// cleanupWorktrees removes stale agent worktrees so var/agents does not grow
// without bound. Busy worktrees are never touched.
func (h *IssueHandler) cleanupWorktrees() {
	agentsDir := filepath.Join(h.Cfg.RootDir, "var", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return
	}

	h.busyMu.Lock()
	busy := len(h.busyIssues) + len(h.busyPRs)
	h.busyMu.Unlock()
	if busy > 0 {
		// Cheap conservative guard: skip cleanup entirely while anything runs,
		// instead of mapping directory names back to claim keys.
		return
	}

	pruned := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < worktreeMaxAge {
			continue
		}
		path := filepath.Join(agentsDir, entry.Name())
		fmt.Printf("[issue-handler] Pruning stale worktree %s\n", path)
		_, _ = h.runCommand(h.Cfg.RootDir, nil, "git", "worktree", "remove", "--force", path)
		_ = os.RemoveAll(path)
		pruned = true
	}
	if pruned {
		_, _ = h.runCommand(h.Cfg.RootDir, nil, "git", "worktree", "prune")
	}
}

func statusLabel(issue *sdk.Issue) string {
	for _, l := range issue.Labels {
		if after, ok := strings.CutPrefix(l.Name, "status:"); ok {
			return after
		}
	}
	return ""
}

func (h *IssueHandler) ProcessIssue(issue *sdk.Issue) error {
	num := issue.Index
	owner, repo := h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo
	fmt.Printf("[issue-handler] Checking issue #%d: %s\n", num, issue.Title)

	if !h.tryClaim(h.busyIssues, num) {
		fmt.Printf("[issue-handler] Issue #%d is already being processed, skipping\n", num)
		return nil
	}
	defer h.release(h.busyIssues, num)

	status := statusLabel(issue)
	switch status {
	case "completed", "failed_max_attempts", "pr_opened", "cancelled", "needs_human":
		return nil
	}

	state := loadStatus(h.Developer, owner, repo, num)

	// in_progress is a lock owned by a previous run; only reclaim when stale.
	if status == "in_progress" {
		timeout := time.Duration(h.Cfg.Issue.InProgressTimeoutSec) * time.Second
		if state.ClaimedAt.IsZero() || time.Since(state.ClaimedAt) < timeout {
			return nil
		}
		fmt.Printf("[issue-handler] Issue #%d in_progress is stale (claimed %s), reclaiming\n", num, state.ClaimedAt)
	}

	// A PR already closing this issue means the work is done here.
	if prs, err := h.Developer.ListOpenPullRequests(owner, repo); err == nil {
		for _, pr := range prs {
			if strings.Contains(pr.Body, fmt.Sprintf("Closes #%d", num)) {
				fmt.Printf("[issue-handler] PR already exists for issue #%d. Marking as pr_opened.\n", num)
				return h.SetStatusLabel(num, "pr_opened")
			}
		}
	}

	if state.Attempts >= h.Cfg.Issue.MaxFixAttempts {
		_ = h.SetStatusLabel(num, "failed_max_attempts")
		return upsertStatus(h.Developer, owner, repo, num, func(s *workStatus) {
			s.Stage = stageFailed
		})
	}

	if status == "failed" {
		cooldown := time.Duration(h.Cfg.Issue.RetryIntervalSec) * time.Second
		if !state.UpdatedAt.IsZero() && time.Since(state.UpdatedAt) < cooldown {
			return nil
		}
	}

	if chatID, ok := telegramChatID(issue.Body); ok {
		_ = h.TelegramSend(chatID, fmt.Sprintf("Started processing issue #%d", num))
	}

	if h.Cfg.Issue.DryRun {
		fmt.Printf("[issue-handler] Dry-run mode for issue #%d\n", num)
		return h.SetStatusLabel(num, "dry_run")
	}

	// Architect-first gate: the developer only starts once the architect's
	// analysis is in the issue body, so the plan stays reviewable.
	if h.Cfg.Issue.Architect.Enabled && !strings.Contains(issue.Body, ArchEnd) {
		return h.RunArchitectStage(issue)
	}

	return h.runDeveloperStage(issue, state)
}

func telegramChatID(body string) (int64, bool) {
	match := regexp.MustCompile(TgChatRegex).FindStringSubmatch(body)
	if len(match) < 2 {
		return 0, false
	}
	chatID, err := strconv.ParseInt(match[1], 10, 64)
	return chatID, err == nil
}

// RunArchitectStage produces the solution analysis and appends it to the issue body.
func (h *IssueHandler) RunArchitectStage(issue *sdk.Issue) error {
	num := issue.Index
	owner, repo := h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo

	if strings.Contains(issue.Body, ArchEnd) {
		return nil
	}

	// Bounded retries: a persistently failing architect must not burn model
	// budget every poll cycle; hand over to a human instead.
	state := loadStatus(h.Developer, owner, repo, num)
	if state.ArchAttempts >= h.Cfg.Issue.Architect.MaxAttempts {
		fmt.Printf("[issue-handler] Architect budget exhausted for issue #%d, needs human\n", num)
		_ = h.SetStatusLabel(num, "needs_human")
		return upsertStatus(h.Developer, owner, repo, num, func(s *workStatus) {
			s.Stage = stageFailed
		})
	}

	_ = upsertStatus(h.Developer, owner, repo, num, func(s *workStatus) {
		s.Stage = stageArchitect
	})

	branch := h.issueBranch(num, issue.Title)
	path := h.worktreePath(fmt.Sprintf("issue-%d", num))
	if err := h.ensureWorktree(branch, path, "origin/"+h.Cfg.Issue.BaseBranch); err != nil {
		return fmt.Errorf("failed to ensure worktree for architect: %w", err)
	}

	promptPath, err := h.writeArchitectPrompt(path, issue)
	if err != nil {
		return fmt.Errorf("failed to write architect prompt: %w", err)
	}

	out, err := h.runAgent(path, promptPath, h.Architect)
	if err != nil {
		_ = upsertStatus(h.Developer, owner, repo, num, func(s *workStatus) {
			s.ArchAttempts++
		})
		return fmt.Errorf("architect agent failed: %w: %s", err, out)
	}

	if max := h.Cfg.Issue.Architect.MaxChars; max > 0 && len(out) > max {
		out = out[:max] + "\n\n(truncated)"
	}

	newBody := issue.Body + "\n\n" + ArchStart + "\n## Solution Architect\n" + out + "\n" + ArchEnd
	if err := h.Architect.UpdateIssueBody(owner, repo, num, newBody); err != nil {
		return fmt.Errorf("failed to store architect analysis: %w", err)
	}
	return nil
}

func (h *IssueHandler) runDeveloperStage(issue *sdk.Issue, state *workStatus) error {
	num := issue.Index
	owner, repo := h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo

	branch := h.issueBranch(num, issue.Title)
	path := h.worktreePath(fmt.Sprintf("issue-%d", num))
	issueType := h.ClassifyIssue(issue)
	skills := h.SelectSkills(issue)

	_ = h.SetStatusLabel(num, "in_progress")
	if err := upsertStatus(h.Developer, owner, repo, num, func(s *workStatus) {
		s.Stage = stageDeveloper
		s.Attempts = state.Attempts + 1
		s.ClaimedAt = time.Now().UTC()
	}); err != nil {
		return fmt.Errorf("failed to claim issue: %w", err)
	}

	if err := h.ensureWorktree(branch, path, "origin/"+h.Cfg.Issue.BaseBranch); err != nil {
		return fmt.Errorf("failed to ensure worktree for developer: %w", err)
	}

	promptPath, err := h.writePrompt(path, issue, issueType, skills, "")
	if err != nil {
		return fmt.Errorf("failed to write developer prompt: %w", err)
	}

	out, err := h.runAgent(path, promptPath, h.Developer)
	if err != nil {
		_ = h.SetStatusLabel(num, "failed")
		return fmt.Errorf("developer agent failed: %w: %s", err, out)
	}

	for fixAttempt := 0; ; fixAttempt++ {
		checkOut, checkErr := h.runChecks(path)
		if checkErr == nil {
			break
		}
		if fixAttempt+1 >= h.Cfg.Issue.MaxFixAttempts {
			_ = h.SetStatusLabel(num, "failed")
			_ = upsertStatus(h.Developer, owner, repo, num, func(s *workStatus) {
				s.Stage = stageFailed
			})
			return fmt.Errorf("checks still failing after %d attempts: %s", fixAttempt+1, checkOut)
		}

		extra := fmt.Sprintf("Checks failed. Fix all failures.\n\n%s", checkOut)
		promptPath, err = h.writePrompt(path, issue, issueType, skills, extra)
		if err != nil {
			return err
		}
		out, err = h.runAgent(path, promptPath, h.Developer)
		if err != nil {
			_ = h.SetStatusLabel(num, "failed")
			return fmt.Errorf("fix agent failed: %w: %s", err, out)
		}
	}

	if err := h.commitAndPush(path, branch, issue, issueType); err != nil {
		_ = h.SetStatusLabel(num, "failed")
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	prURL, err := h.createPR(issue, branch, issueType, skills)
	if err != nil {
		_ = h.SetStatusLabel(num, "failed")
		return fmt.Errorf("failed to create PR: %w", err)
	}

	_ = h.SetStatusLabel(num, "pr_opened")
	return upsertStatus(h.Developer, owner, repo, num, func(s *workStatus) {
		s.Stage = stagePROpened
		s.PRURL = prURL
	})
}

func (h *IssueHandler) SetStatusLabel(num int64, status string) error {
	owner, repo := h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo
	issue, err := h.Developer.GetIssue(owner, repo, num)
	if err != nil {
		return err
	}
	for _, l := range issue.Labels {
		if strings.HasPrefix(l.Name, "status:") && l.Name != "status:"+status {
			_ = h.Developer.RemoveIssueLabel(owner, repo, num, l.Name)
		}
	}
	return h.Developer.AddIssueLabel(owner, repo, num, "status:"+status)
}

// ensureSafeDirectory marks dir as a git safe.directory exactly once; the
// previous unconditional --add grew the global config by one line per run.
func (h *IssueHandler) ensureSafeDirectory(dir string) {
	existing, _ := h.runCommand(h.Cfg.RootDir, nil, "git", "config", "--global", "--get-all", "safe.directory")
	if slices.Contains(strings.Split(existing, "\n"), dir) {
		return
	}
	_, _ = h.runCommand(h.Cfg.RootDir, nil, "git", "config", "--global", "--add", "safe.directory", dir)
}

// ensureWorktree (re)creates a worktree for branch at path, based on baseRef.
// Callers must hold the busy claim for the issue/PR owning the path.
func (h *IssueHandler) ensureWorktree(branch, path, baseRef string) error {
	h.ensureSafeDirectory(h.Cfg.RootDir)
	h.ensureSafeDirectory("/workspace")

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		_, _ = h.runCommand(h.Cfg.RootDir, nil, "git", "worktree", "remove", "--force", path)
	}

	_, _ = h.runCommand(h.Cfg.RootDir, nil, "git", "fetch", "origin")
	out, err := h.runCommand(h.Cfg.RootDir, nil, "git", "worktree", "add", "-B", branch, path, baseRef)
	if err != nil {
		return fmt.Errorf("failed to prepare worktree: %s: %w", out, err)
	}
	return nil
}

// defaultCommandTimeout bounds git and check subprocesses; agent runs use the
// configurable ISSUE_AGENT_TIMEOUT_SEC instead.
const defaultCommandTimeout = 10 * time.Minute

func (h *IssueHandler) runCommand(cwd string, extraEnv []string, name string, args ...string) (string, error) {
	return h.runCommandTimeout(defaultCommandTimeout, cwd, extraEnv, name, args...)
}

// runCommandTimeout executes a subprocess with a hard deadline so a hung agent
// or check can never block the poll loop and hold busy claims forever.
func (h *IssueHandler) runCommandTimeout(timeout time.Duration, cwd string, extraEnv []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.WaitDelay = 10 * time.Second

	out, err := cmd.CombinedOutput()
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("%s timed out after %s", name, timeout)
	}
	return strings.TrimSpace(string(out)), err
}

func (h *IssueHandler) agentTimeout() time.Duration {
	if sec := h.Cfg.Issue.AgentTimeoutSec; sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return 30 * time.Minute
}

// roleIdentityEnv returns env vars attributing git commits to the role's Gitea user.
func roleIdentityEnv(role gitea.Client) []string {
	user, err := role.CurrentUser()
	if err != nil {
		return nil
	}
	email := user.Email
	if email == "" {
		email = user.UserName + "@ai-fabric.local"
	}
	return []string{
		"GIT_AUTHOR_NAME=" + user.UserName,
		"GIT_AUTHOR_EMAIL=" + email,
		"GIT_COMMITTER_NAME=" + user.UserName,
		"GIT_COMMITTER_EMAIL=" + email,
	}
}

func (h *IssueHandler) gitIdentityEnv() []string {
	return roleIdentityEnv(h.Developer)
}

// gitPushEnv injects the developer token as an HTTP header via GIT_CONFIG_*
// env vars so the token never appears in process arguments or on-disk config.
func (h *IssueHandler) gitPushEnv() []string {
	token := h.Cfg.Gitea.HandlerToken
	if token == "" {
		token = h.Cfg.Gitea.Token
	}
	if token == "" || !strings.HasPrefix(h.Cfg.Gitea.BaseURL, "http") {
		return nil
	}
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: token " + token,
	}
}

// pushURL is the repository URL on the configured Gitea instance.
func (h *IssueHandler) pushURL() string {
	return fmt.Sprintf("%s/%s/%s.git",
		strings.TrimRight(h.Cfg.Gitea.BaseURL, "/"), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo)
}

// gitPush pushes the local branch to the remote branch on the configured repo.
func (h *IssueHandler) gitPush(path, localBranch, remoteBranch string) error {
	env := append(h.gitIdentityEnv(), h.gitPushEnv()...)
	out, err := h.runCommand(path, env, "git", "push", h.pushURL(), localBranch+":"+remoteBranch)
	if err != nil {
		return fmt.Errorf("push failed: %s: %w", out, err)
	}
	return nil
}

func (h *IssueHandler) commitAndPush(path, branch string, issue *sdk.Issue, issueType string) error {
	status, err := h.runCommand(path, nil, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("cannot inspect git status: %w", err)
	}
	if status == "" {
		// The agent may have committed already; push if the branch has new commits.
		diff, _ := h.runCommand(path, nil, "git", "rev-list", "HEAD", "^origin/"+h.Cfg.Issue.BaseBranch)
		if diff != "" {
			fmt.Printf("[issue-handler] Agent already committed changes. Pushing...\n")
			return h.gitPush(path, branch, branch)
		}
		return fmt.Errorf("agent made no file changes")
	}

	if _, err = h.runCommand(path, nil, "git", "add", "."); err != nil {
		return err
	}

	prefix := "feat"
	if issueType == "bug" {
		prefix = "fix"
	}
	title := issue.Title
	if len(title) > 60 {
		title = title[:60]
	}
	msg := fmt.Sprintf("%s(issue #%d): %s", prefix, issue.Index, title)

	if out, err := h.runCommand(path, h.gitIdentityEnv(), "git", "commit", "-m", msg); err != nil {
		return fmt.Errorf("commit failed: %s: %w", out, err)
	}

	return h.gitPush(path, branch, branch)
}

func (h *IssueHandler) createPR(issue *sdk.Issue, branch, issueType string, skills []string) (string, error) {
	owner, repo := h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo

	if prs, err := h.Developer.ListOpenPullRequests(owner, repo); err == nil {
		for _, pr := range prs {
			if pr.Head != nil && pr.Head.Ref == branch {
				return pr.HTMLURL, nil
			}
		}
	}

	var skillLines []string
	for _, s := range skills {
		skillLines = append(skillLines, fmt.Sprintf("  - `%s`", s))
	}

	body := fmt.Sprintf(`## Problem
Automated implementation for issue #%d: %s

## Solution
Agent-based implementation using branch %s with scoped changes.

## Risks
- Generated changes may require human review for edge cases.
- CI and policy checks are enforced before merge.

## Test Plan
- [x] ./bin/fmt.sh
- [x] ./bin/lint.sh
- [x] ./bin/test.sh

## Rollback
Revert branch %s merge commit if needed.

## Issue Link
Closes #%d

## AI Notes
- Type: %s
- Automated by issue handler.
- Skills/docs considered:
%s
`, issue.Index, issue.Title, branch, branch, issue.Index, issueType, strings.Join(skillLines, "\n"))

	pr, err := h.Developer.CreatePullRequest(owner, repo, sdk.CreatePullRequestOption{
		Title: fmt.Sprintf("[agent] %s", issue.Title),
		Head:  branch,
		Base:  h.Cfg.Issue.BaseBranch,
		Body:  body,
	})
	if err != nil {
		return "", err
	}
	return pr.HTMLURL, nil
}

func (h *IssueHandler) writePrompt(path string, issue *sdk.Issue, issueType string, skills []string, extra string) (string, error) {
	promptPath := filepath.Join(path, ".issue-agent-prompt.md")
	var skillLines []string
	for _, s := range skills {
		skillLines = append(skillLines, fmt.Sprintf("- `%s`", s))
	}
	body := issue.Body
	if body == "" {
		body = "(empty)"
	}

	prompt := fmt.Sprintf(`# Issue #%d execution

Type: %s
Title: %s

## Task
Implement the issue end-to-end in this repository:
- follow the Solution Architect section in the issue body; it defines the structure to implement
- complete every item of the architect's "## Tasks" checklist; list the completed tasks in your final summary
- write or update use-case level tests first where applicable
- implement minimal safe changes
- run ./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh
- commit changes

## Issue body
%s

## Relevant skills/docs to read first
%s

## Constraints (inviolable, see docs/skills/engineering-principles.md)
- Keep changes scoped to this issue; no work for work's sake
- Reuse existing packages, libraries and upstream SDK types before writing new code
- No mock-based or isolated unit tests: only use-case, API and system-level tests
- Prefer YAML over JSON for configuration and exchange formats
- Follow PR template and workflow policies
`, issue.Index, issueType, issue.Title, body, strings.Join(skillLines, "\n"))

	if extra != "" {
		prompt += fmt.Sprintf("\n## Additional context\n%s\n", extra)
	}

	err := os.WriteFile(promptPath, []byte(prompt), 0644)
	return promptPath, err
}

func (h *IssueHandler) writeArchitectPrompt(path string, issue *sdk.Issue) (string, error) {
	promptPath := filepath.Join(path, ".issue-architect-prompt.md")
	var skillLines []string
	for _, s := range h.SelectSkills(issue) {
		skillLines = append(skillLines, fmt.Sprintf("- `%s`", s))
	}
	body := issue.Body
	if body == "" {
		body = "(empty)"
	}

	prompt := fmt.Sprintf(`# Solution Architect analysis for issue #%d

Type: %s
Title: %s

You are acting as Solution Architect.
Produce a concise markdown analysis with this exact structure:

## Possible Solutions
- Option A: ...
- Option B: ...

## Recommended Approach
- Why this option is preferred
- Risks
- Dependencies

## Implementation Structure
- Packages/files the developer must touch and the boundaries to respect
- Existing code, libraries and SDK types to reuse (reuse-first is mandatory)

## Tasks
- [ ] concrete, verifiable implementation steps as a markdown checklist
- [ ] each task small enough to confirm done by looking at the diff
(this checklist is the work breakdown; the developer must complete every item)

## Estimation
- Complexity: (S|M|L)
- Estimated effort: <time>
- Test scope: use-case/API/system tests only (no mocks, no isolated unit tests)

## Required Skills/Context
- list relevant docs/skills from repository

Do not include any content outside these sections.
The design must follow docs/skills/engineering-principles.md (Occam's razor,
YAGNI, idiomatic Go, 3-tier boundaries, no duplication, reuse-first).

## Issue body
%s

## Relevant skills/docs
%s
`, issue.Index, h.ClassifyIssue(issue), issue.Title, body, strings.Join(skillLines, "\n"))

	err := os.WriteFile(promptPath, []byte(prompt), 0644)
	return promptPath, err
}

// runAgent executes the coding agent in path with the work model and the git
// identity of the given role's Gitea user.
func (h *IssueHandler) runAgent(path, promptPath string, role gitea.Client) (string, error) {
	agentBin := h.Cfg.Issue.AgentBin
	if agentBin == "" {
		agentBin = "agent"
	}

	promptData, err := os.ReadFile(promptPath)
	if err != nil {
		return "", err
	}

	args := []string{"-p", "--workspace", path, "--trust"}
	if model := h.workModel(); model != "" {
		args = append(args, "--model", model)
	}
	if h.Cfg.Issue.AgentExtraArgs != "" {
		args = append(args, strings.Fields(h.Cfg.Issue.AgentExtraArgs)...)
	}
	args = append(args, string(promptData))

	return h.runCommandTimeout(h.agentTimeout(), path, roleIdentityEnv(role), agentBin, args...)
}

func (h *IssueHandler) runChecks(path string) (string, error) {
	return h.runCommand(path, nil, "/bin/bash", "-lc", "./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh")
}

func (h *IssueHandler) issueBranch(issueNumber int64, title string) string {
	reg := regexp.MustCompile("[^a-z0-9]+")
	slug := strings.Trim(reg.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	if slug == "" {
		slug = "task"
	}
	return fmt.Sprintf("issue/%d-%s", issueNumber, slug)
}

func (h *IssueHandler) worktreePath(name string) string {
	return filepath.Join(h.Cfg.RootDir, "var", "agents", name)
}
