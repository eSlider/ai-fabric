package fabric

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"produktor.io/ai-fabric/pkg/gitea"
	"produktor.io/ai-fabric/pkg/system"
)

const (
	ArchStart   = "<!-- ai-fabric:solution-architect:start -->"
	ArchEnd     = "<!-- ai-fabric:solution-architect:end -->"
	TgChatRegex = `<!--\s*ai-fabric:telegram-chat-id:(-?\d+)\s*-->`
)

type IssueHandler struct {
	Cfg         *system.Config
	GiteaClient gitea.Client
}

func NewIssueHandler(cfg *system.Config) *IssueHandler {
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
	issueNum := strconv.Itoa(extractIssueNumber(issue["number"]))
	fmt.Printf("[issue-handler] Checking issue #%s\n", issueNum)

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
		if n := extractIssueNumber(issue["number"]); n > 0 {
			_ = h.GiteaClient.CreateIssueComment(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, n, "[issue-handler] Max fix attempts reached.")
		}
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

	if n := extractIssueNumber(issue["number"]); n > 0 {
		_ = h.GiteaClient.CreateIssueComment(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, n, "[issue-handler] Claimed issue. Starting automated processing.")
	}

	// Placeholder for full implementation orchestration.
	fmt.Printf("[issue-handler] Processing #%s: %s\n", issueNum, issue["title"])

	issueState["status"] = "completed"
	issueState["attempts"] = attempts + 1
	issueState["last_attempt"] = time.Now().Format(time.RFC3339)

	if n := extractIssueNumber(issue["number"]); n > 0 {
		_ = h.GiteaClient.CreateIssueComment(context.Background(), h.Cfg.Gitea.Owner, h.Cfg.Gitea.Repo, n, "[issue-handler] Processing completed.")
	}

	return nil
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
