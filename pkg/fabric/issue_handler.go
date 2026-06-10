package fabric

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"example.org/ai-fabric/internal/config"
	"example.org/ai-fabric/pkg/gitea"
)

const (
	ArchStart   = "<!-- ai-fabric:solution-architect:start -->"
	ArchEnd     = "<!-- ai-fabric:solution-architect:end -->"
	TgChatRegex = `<!--\s*ai-fabric:telegram-chat-id:(-?\d+)\s*-->`
)

type IssueHandler struct {
	Cfg         *config.Config
	GiteaClient gitea.Client
}

func NewIssueHandler(cfg *config.Config) *IssueHandler {
	return &IssueHandler{
		Cfg:         cfg,
		GiteaClient: gitea.NewService(cfg.Gitea, cfg.RootDir, cfg.TeaConfigDir),
	}
}

func (h *IssueHandler) LoadState() (map[string]interface{}, error) {
	state := make(map[string]interface{})
	if _, err := os.Stat(h.Cfg.StatePath); os.IsNotExist(err) {
		return state, nil
	}
	data, err := os.ReadFile(h.Cfg.StatePath)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func (h *IssueHandler) SaveState(state map[string]interface{}) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.Cfg.StatePath, data, 0644)
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

func (h *IssueHandler) ListOpenIssues() ([]map[string]interface{}, error) {
	return h.GiteaClient.ListOpenIssues(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo)
}

func (h *IssueHandler) ClassifyIssue(issue map[string]interface{}) string {
	title, _ := issue["title"].(string)
	body, _ := issue["body"].(string)
	text := strings.ToLower(title + "\n" + body)
	bugHints := []string{"bug", "error", "broken", "fail", "exception", "crash", "regression", "fix"}
	for _, hint := range bugHints {
		if strings.Contains(text, hint) {
			return "bug"
		}
	}
	return "feature"
}

func (h *IssueHandler) SelectSkills(issue map[string]interface{}) []string {
	title, _ := issue["title"].(string)
	body, _ := issue["body"].(string)
	text := strings.ToLower(title + "\n" + body)
	skills := []string{
		"docs/skills/agent-guidelines.md",
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
		if strings.Contains(text, key) {
			found := false
			for _, s := range skills {
				if s == path {
					found = true
					break
				}
			}
			if !found {
				skills = append(skills, path)
			}
		}
	}
	return skills
}

func (h *IssueHandler) RunOnce(targetIssue int) {
	fmt.Printf("[issue-handler] Starting cycle... targetIssue=%d\n", targetIssue)
	state, err := h.LoadState()
	if err != nil {
		fmt.Printf("[issue-handler] Failed to load state: %v\n", err)
		return
	}

	var issues []map[string]interface{}
	if targetIssue > 0 {
		issue, reqErr := h.GiteaClient.GetIssue(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, targetIssue)
		if reqErr == nil {
			if issue["state"] == "open" && issue["pull_request"] == nil {
				issues = append(issues, issue)
			}
		}
	} else {
		issues, err = h.ListOpenIssues()
	}

	if err != nil {
		fmt.Printf("[issue-handler] Failed to list issues: %v\n", err)
		return
	}

	for _, issue := range issues {
		err := h.ProcessIssue(issue, state)
		if err != nil {
			fmt.Printf("[issue-handler] Error processing issue %v: %v\n", issue["number"], err)
		}
	}

	_ = h.SaveState(state)
}

func (h *IssueHandler) ProcessIssue(issue map[string]interface{}, state map[string]interface{}) error {
	num := extractIssueNumber(issue["number"])
	issueNum := strconv.Itoa(num)
	title, _ := issue["title"].(string)
	fmt.Printf("[issue-handler] Checking issue #%s: %s\n", issueNum, title)

	issueKey := "issue-" + issueNum
	issueState, _ := state[issueKey].(map[string]interface{})
	if issueState == nil {
		issueState = make(map[string]interface{})
		state[issueKey] = issueState
	}

	status, _ := issueState["status"].(string)
	if status == "completed" || status == "failed_max_attempts" || status == "pr_opened" || status == "cancelled" {
		return nil
	}

	if status == "failed" {
		lastAttempt, _ := issueState["last_attempt"].(string)
		if ts, err := time.Parse(time.RFC3339, lastAttempt); err == nil {
			if time.Since(ts) < time.Duration(h.Cfg.Issue.IssueBot.RetryIntervalSec)*time.Second {
				return nil
			}
		}
	}

	attempts, _ := issueState["attempts"].(float64)
	if int(attempts) >= h.Cfg.Issue.MaxFixAttempts {
		issueState["status"] = "failed_max_attempts"
		_ = h.GiteaClient.CreateIssueComment(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, num, "[issue-handler] Max fix attempts reached.")
		return nil
	}

	// Telegram notification check
	body, _ := issue["body"].(string)
	re := regexp.MustCompile(TgChatRegex)
	match := re.FindStringSubmatch(body)
	if len(match) > 1 {
		chatID, _ := strconv.ParseInt(match[1], 10, 64)
		_ = h.TelegramSend(chatID, fmt.Sprintf("Started processing issue #%s", issueNum))
	}

	if h.Cfg.Issue.DryRun {
		issueState["status"] = "dry_run"
		issueState["attempts"] = attempts + 1
		issueState["last_attempt"] = time.Now().Format(time.RFC3339)
		fmt.Printf("[issue-handler] Dry-run mode for issue #%s\n", issueNum)
		return nil
	}

	branch := h.issueBranch(num, title)
	path := h.worktreePath(num)
	issueType := h.ClassifyIssue(issue)
	skills := h.SelectSkills(issue)

	issueState["status"] = "in_progress"
	issueState["last_attempt"] = time.Now().Format(time.RFC3339)
	_ = h.SaveState(state)

	// Architect stage
	architectDone, _ := issueState["architect_done"].(bool)
	if !architectDone && h.Cfg.Issue.Architect.Enabled {
		_ = h.GiteaClient.CreateIssueComment(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, num, "[issue-handler] Running Solution Architect analysis and updating issue body.")

		if err := h.ensureWorktree(branch, path); err != nil {
			return fmt.Errorf("failed to ensure worktree for architect: %w", err)
		}

		promptPath, err := h.writeArchitectPrompt(path, issue, issueType, skills)
		if err != nil {
			return fmt.Errorf("failed to write architect prompt: %w", err)
		}

		out, err := h.runAgent(path, promptPath)
		if err != nil {
			return fmt.Errorf("architect agent failed: %w: %s", err, out)
		}

		// Update issue body with architect's analysis
		newBody := body + "\n\n" + ArchStart + "\n## Solution Architect\n" + out + "\n" + ArchEnd
		_ = h.GiteaClient.UpdateIssueBody(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, num, newBody)

		issueState["architect_done"] = true
		_ = h.SaveState(state)

		// Refresh issue data
		issue, _ = h.GiteaClient.GetIssue(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, num)
	}

	_ = h.GiteaClient.CreateIssueComment(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, num, fmt.Sprintf("[issue-handler] Claimed issue. Starting developer implementation on branch `%s`.", branch))

	if err := h.ensureWorktree(branch, path); err != nil {
		return fmt.Errorf("failed to ensure worktree for developer: %w", err)
	}

	promptPath, err := h.writePrompt(path, issue, issueType, skills, "")
	if err != nil {
		return fmt.Errorf("failed to write developer prompt: %w", err)
	}

	out, err := h.runAgent(path, promptPath)
	if err != nil {
		return fmt.Errorf("developer agent failed: %w: %s", err, out)
	}

	// Fix loop
	fixAttempt := 0
	for fixAttempt < h.Cfg.Issue.MaxFixAttempts {
		checkOut, checkErr := h.runChecks(path)
		if checkErr == nil {
			break
		}
		fixAttempt++
		if fixAttempt >= h.Cfg.Issue.MaxFixAttempts {
			return fmt.Errorf("checks still failing after %d attempts: %s", fixAttempt, checkOut)
		}

		_ = h.GiteaClient.CreateIssueComment(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, num, fmt.Sprintf("[issue-handler] Checks failed (attempt %d). Retrying implementation...", fixAttempt))

		extra := fmt.Sprintf("Checks failed. Fix all failures.\n\n%s", checkOut)
		promptPath, err = h.writePrompt(path, issue, issueType, skills, extra)
		if err != nil {
			return err
		}
		out, err = h.runAgent(path, promptPath)
		if err != nil {
			return fmt.Errorf("fix agent failed: %w: %s", err, out)
		}
	}

	if err := h.commitAndPush(path, branch, issue, issueType); err != nil {
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	prURL, err := h.createPR(issue, branch, issueType, skills)
	if err != nil {
		return fmt.Errorf("failed to create PR: %w", err)
	}

	_ = h.GiteaClient.CreateIssueComment(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, num, fmt.Sprintf("[issue-handler] Opened PR: %s", prURL))

	issueState["status"] = "pr_opened"
	issueState["pr_url"] = prURL
	issueState["branch"] = branch
	issueState["attempts"] = attempts + 1
	_ = h.SaveState(state)

	return nil
}

func (h *IssueHandler) ensureWorktree(branch string, path string) error {
	// Add safe directory configs
	_, _ = h.runCommand(h.Cfg.RootDir, "git", "config", "--global", "--add", "safe.directory", h.Cfg.RootDir)
	_, _ = h.runCommand(h.Cfg.RootDir, "git", "config", "--global", "--add", "safe.directory", "/workspace")

	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		_, _ = h.runCommand(h.Cfg.RootDir, "git", "worktree", "remove", "--force", path)
	}

	_, _ = h.runCommand(h.Cfg.RootDir, "git", "fetch", "origin")
	out, err := h.runCommand(h.Cfg.RootDir, "git", "worktree", "add", "-B", branch, path, "origin/"+h.Cfg.Issue.BaseBranch)
	if err != nil {
		return fmt.Errorf("failed to prepare worktree: %s: %w", out, err)
	}
	return nil
}

func (h *IssueHandler) runCommand(cwd string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (h *IssueHandler) writePrompt(path string, issue map[string]interface{}, issueType string, skills []string, extra string) (string, error) {
	promptPath := filepath.Join(path, ".issue-agent-prompt.md")
	var skillLines []string
	for _, s := range skills {
		skillLines = append(skillLines, fmt.Sprintf("- `%s`", s))
	}
	body, _ := issue["body"].(string)
	if body == "" {
		body = "(empty)"
	}
	num := extractIssueNumber(issue["number"])
	title, _ := issue["title"].(string)

	prompt := fmt.Sprintf(`# Issue #%d execution

Type: %s
Title: %s

## Task
Implement the issue end-to-end in this repository:
- write or update tests first where applicable
- implement minimal safe changes
- run ./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh
- commit changes
- push branch

## Issue body
%s

## Relevant skills/docs to read first
%s

## Constraints
- Keep changes scoped to this issue
- Follow PR template and workflow policies
- If CI fails, fix and retry until green within attempt limit
`, num, issueType, title, body, strings.Join(skillLines, "\n"))

	if extra != "" {
		prompt += fmt.Sprintf("\n## Additional context\n%s\n", extra)
	}

	err := os.WriteFile(promptPath, []byte(prompt), 0644)
	return promptPath, err
}

func (h *IssueHandler) writeArchitectPrompt(path string, issue map[string]interface{}, issueType string, skills []string) (string, error) {
	promptPath := filepath.Join(path, ".issue-architect-prompt.md")
	var skillLines []string
	for _, s := range skills {
		skillLines = append(skillLines, fmt.Sprintf("- `%s`", s))
	}
	body, _ := issue["body"].(string)
	if body == "" {
		body = "(empty)"
	}
	num := extractIssueNumber(issue["number"])
	title, _ := issue["title"].(string)

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

## Estimation
- Complexity: (S|M|L)
- Estimated effort: <time>
- Test scope: <brief>

## Required Skills/Context
- list relevant docs/skills from repository

Do not include any content outside these sections.

## Issue body
%s

## Relevant skills/docs
%s
`, num, issueType, title, body, strings.Join(skillLines, "\n"))

	err := os.WriteFile(promptPath, []byte(prompt), 0644)
	return promptPath, err
}

func (h *IssueHandler) runAgent(path string, promptPath string) (string, error) {
	agentBin := h.Cfg.Issue.AgentBin
	if agentBin == "" {
		agentBin = "agent"
	}

	promptData, err := os.ReadFile(promptPath)
	if err != nil {
		return "", err
	}

	args := []string{"-p", "--workspace", path, "--trust", string(promptData)}
	if h.Cfg.Issue.AgentExtraArgs != "" {
		extra := strings.Fields(h.Cfg.Issue.AgentExtraArgs)
		args = append([]string{"-p", "--workspace", path, "--trust"}, extra...)
		args = append(args, string(promptData))
	}

	out, err := h.runCommand(path, agentBin, args...)
	return out, err
}

func (h *IssueHandler) runChecks(path string) (string, error) {
	// Equivalent to: /bin/bash -lc "./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh"
	return h.runCommand(path, "/bin/bash", "-lc", "./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh")
}

func (h *IssueHandler) commitAndPush(path string, branch string, issue map[string]interface{}, issueType string) error {
	status, err := h.runCommand(path, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("cannot inspect git status: %w", err)
	}
	if status == "" {
		// Check if we have new commits compared to base branch
		diff, _ := h.runCommand(path, "git", "rev-list", "HEAD", "^origin/"+h.Cfg.Issue.BaseBranch)
		if diff != "" {
			fmt.Printf("[issue-handler] Agent already committed changes. Pushing...\n")
			_, err = h.runCommand(path, "git", "push", "-u", "origin", branch)
			if err != nil {
				return fmt.Errorf("push failed: %w", err)
			}
			return nil
		}
		return fmt.Errorf("agent made no file changes")
	}

	_, err = h.runCommand(path, "git", "add", ".")
	if err != nil {
		return err
	}

	prefix := "feat"
	if issueType == "bug" {
		prefix = "fix"
	}
	num := extractIssueNumber(issue["number"])
	title, _ := issue["title"].(string)
	if len(title) > 60 {
		title = title[:60]
	}
	msg := fmt.Sprintf("%s(issue #%d): %s", prefix, num, title)

	_, err = h.runCommand(path, "git", "commit", "-m", msg)
	if err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	_, err = h.runCommand(path, "git", "push", "-u", "origin", branch)
	if err != nil {
		return fmt.Errorf("push failed: %w", err)
	}

	return nil
}

func (h *IssueHandler) createPR(issue map[string]interface{}, branch string, issueType string, skills []string) (string, error) {
	// Check for existing PR
	prs, err := h.GiteaClient.ListPullRequests(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, "open")
	if err == nil {
		for _, pr := range prs {
			head, _ := pr["head"].(map[string]interface{})
			if head != nil {
				ref, _ := head["ref"].(string)
				if ref == branch {
					url, _ := pr["html_url"].(string)
					return url, nil
				}
			}
		}
	}

	num := extractIssueNumber(issue["number"])
	title, _ := issue["title"].(string)

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
`, num, title, branch, branch, num, issueType, strings.Join(skillLines, "\n"))

	payload := map[string]interface{}{
		"title": fmt.Sprintf("[agent] %s", title),
		"head":  branch,
		"base":  h.Cfg.Issue.BaseBranch,
		"body":  body,
	}

	pr, err := h.GiteaClient.CreatePullRequest(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, payload)
	if err != nil {
		return "", err
	}

	url, _ := pr["html_url"].(string)
	return url, nil
}

func (h *IssueHandler) issueBranch(issueNumber int, title string) string {
	reg, _ := regexp.Compile("[^a-z0-9]+")
	slug := strings.Trim(reg.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	if slug == "" {
		slug = "task"
	}
	return fmt.Sprintf("issue/%d-%s", issueNumber, slug)
}

func (h *IssueHandler) worktreePath(issueNumber int) string {
	return filepath.Join(h.Cfg.RootDir, "var", "agents", fmt.Sprintf("issue-%d", issueNumber))
}

func extractIssueNumber(raw interface{}) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}
