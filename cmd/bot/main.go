package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type config struct {
	Token            string
	AllowedChatIDs   map[string]bool
	AllowedUsers     map[string]bool
	GiteaBaseURL     string
	GiteaOwner       string
	GiteaRepo        string
	GiteaToken       string
	ProjectListLimit int
}

func main() {
	cfg := loadConfig()
	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}

	bot, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init telegram bot: %v\n", err)
		os.Exit(1)
	}

	updates := bot.GetUpdatesChan(tgbotapi.NewUpdate(0))
	for update := range updates {
		if update.Message == nil {
			continue
		}
		if !isAllowed(cfg, update.Message) {
			_ = reply(bot, update.Message.Chat.ID, "Access denied.")
			continue
		}

		text := strings.TrimSpace(update.Message.Text)
		if text == "" {
			continue
		}
		cmd, arg := splitCommand(text)
		switch cmd {
		case "/status":
			_ = reply(bot, update.Message.Chat.ID, "eSlider's fabric bot is running.")
		case "/health":
			_ = reply(bot, update.Message.Chat.ID, health(cfg))
		case "/projects":
			msg, err := listProjects(cfg)
			if err != nil {
				msg = "Failed to list projects: " + err.Error()
			}
			_ = reply(bot, update.Message.Chat.ID, msg)
		case "/task":
			msg, err := createTaskIssue(cfg, arg, update.Message.Chat.ID)
			if err != nil {
				msg = "Failed to create task: " + err.Error()
			}
			_ = reply(bot, update.Message.Chat.ID, msg)
		case "/checks":
			out := runScript("bin/fmt.sh", 120*time.Second)
			out += "\n\n" + runScript("bin/lint.sh", 120*time.Second)
			out += "\n\n" + runScript("bin/test.sh", 120*time.Second)
			_ = reply(bot, update.Message.Chat.ID, trimLen(out, 3900))
		case "/up":
			_ = reply(bot, update.Message.Chat.ID, runScript("bin/up.sh", 180*time.Second))
		case "/down":
			_ = reply(bot, update.Message.Chat.ID, runScript("bin/down.sh", 180*time.Second))
		case "/logs":
			service := strings.TrimSpace(arg)
			if service == "" {
				_ = reply(bot, update.Message.Chat.ID, "Usage: /logs <service>")
				continue
			}
			_ = reply(bot, update.Message.Chat.ID, runComposeLogs(service))
		default:
			_ = reply(bot, update.Message.Chat.ID, "Unknown command.")
		}
	}
}

func loadConfig() config {
	return config{
		Token:            strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		AllowedChatIDs:   parseSet(os.Getenv("TELEGRAM_ALLOWED_CHAT_IDS")),
		AllowedUsers:     parseSet(strings.ToLower(os.Getenv("TELEGRAM_ALLOWED_USERNAMES"))),
		GiteaBaseURL:     strings.TrimRight(strings.TrimSpace(os.Getenv("GITEA_BOT_BASE_URL")), "/"),
		GiteaOwner:       strings.TrimSpace(os.Getenv("GITEA_BOT_OWNER")),
		GiteaRepo:        strings.TrimSpace(os.Getenv("GITEA_BOT_REPO")),
		GiteaToken:       strings.TrimSpace(os.Getenv("GITEA_BOT_TOKEN")),
		ProjectListLimit: 20,
	}
}

func parseSet(v string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func isAllowed(cfg config, msg *tgbotapi.Message) bool {
	// Empty allowlists mean open access.
	if len(cfg.AllowedChatIDs) == 0 && len(cfg.AllowedUsers) == 0 {
		return true
	}
	if len(cfg.AllowedChatIDs) > 0 && cfg.AllowedChatIDs[fmt.Sprintf("%d", msg.Chat.ID)] {
		return true
	}
	if msg.From != nil && len(cfg.AllowedUsers) > 0 && cfg.AllowedUsers[strings.ToLower(msg.From.UserName)] {
		return true
	}
	return false
}

func splitCommand(text string) (string, string) {
	parts := strings.SplitN(text, " ", 2)
	cmd := strings.ToLower(strings.TrimSpace(parts[0]))
	if len(parts) == 1 {
		return cmd, ""
	}
	return cmd, strings.TrimSpace(parts[1])
}

func reply(bot *tgbotapi.BotAPI, chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, trimLen(text, 4000))
	_, err := bot.Send(msg)
	return err
}

func trimLen(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func health(cfg config) string {
	if cfg.GiteaBaseURL == "" {
		return "GITEA_BOT_BASE_URL is not configured."
	}
	healthURL := cfg.GiteaBaseURL + "/api/healthz"
	resp, err := http.Get(healthURL)
	if err != nil {
		if fallbackBaseURL, ok := fallbackBaseURLForDNSError(cfg.GiteaBaseURL, err); ok {
			resp, err = http.Get(strings.Replace(healthURL, cfg.GiteaBaseURL, fallbackBaseURL, 1))
		}
	}
	if err != nil {
		return "Health check failed: " + err.Error()
	}
	defer resp.Body.Close()
	return fmt.Sprintf("Gitea health status: %d", resp.StatusCode)
}

func runScript(path string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("%s failed: %v\n%s", path, err, string(out))
	}
	return fmt.Sprintf("%s ok\n%s", path, string(out))
}

func runComposeLogs(service string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "--tail", "80", service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("logs failed: %v\n%s", err, string(out))
	}
	return string(out)
}

func listProjects(cfg config) (string, error) {
	if cfg.GiteaBaseURL == "" || cfg.GiteaToken == "" || cfg.GiteaOwner == "" {
		return "", fmt.Errorf("gitea project variables are not fully configured")
	}

	u := fmt.Sprintf("%s/api/v1/orgs/%s/repos?limit=%d", cfg.GiteaBaseURL, url.PathEscape(cfg.GiteaOwner), cfg.ProjectListLimit)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+cfg.GiteaToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if fallbackBaseURL, ok := fallbackBaseURLForDNSError(cfg.GiteaBaseURL, err); ok {
			u = strings.Replace(u, cfg.GiteaBaseURL, fallbackBaseURL, 1)
			req, err = http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("Authorization", "token "+cfg.GiteaToken)
			resp, err = http.DefaultClient.Do(req)
		}
	}
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gitea error: %d %s", resp.StatusCode, string(body))
	}

	var repos []struct {
		Name string `json:"name"`
		URL  string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return "", err
	}
	if len(repos) == 0 {
		return "No projects found.", nil
	}

	var b strings.Builder
	b.WriteString("Projects:\n")
	for _, r := range repos {
		b.WriteString("- " + r.Name)
		if r.URL != "" {
			b.WriteString(" (" + r.URL + ")")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func createTaskIssue(cfg config, description string, chatID int64) (string, error) {
	if description == "" {
		return "Usage: /task <description>", nil
	}
	if cfg.GiteaBaseURL == "" || cfg.GiteaToken == "" || cfg.GiteaOwner == "" || cfg.GiteaRepo == "" {
		return "", fmt.Errorf("gitea issue variables are not fully configured")
	}

	body := fmt.Sprintf("%s\n\n<!-- ai-fabric:telegram-chat-id:%d -->", description, chatID)
	payload := map[string]any{
		"title": "[task] " + trimLen(description, 90),
		"body":  body,
	}
	data, _ := json.Marshal(payload)

	u := fmt.Sprintf("%s/api/v1/repos/%s/%s/issues", cfg.GiteaBaseURL, url.PathEscape(cfg.GiteaOwner), url.PathEscape(cfg.GiteaRepo))
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+cfg.GiteaToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if fallbackBaseURL, ok := fallbackBaseURLForDNSError(cfg.GiteaBaseURL, err); ok {
			u = strings.Replace(u, cfg.GiteaBaseURL, fallbackBaseURL, 1)
			req, err = http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
			if err != nil {
				return "", err
			}
			req.Header.Set("Authorization", "token "+cfg.GiteaToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err = http.DefaultClient.Do(req)
		}
	}
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bodyRaw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gitea error: %d %s", resp.StatusCode, string(bodyRaw))
	}

	var issue struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return "", err
	}
	return fmt.Sprintf("Created issue #%d\n%s", issue.Number, issue.HTMLURL), nil
}

func fallbackBaseURLForDNSError(baseURL string, err error) (string, bool) {
	if err == nil || baseURL == "" {
		return "", false
	}
	base, parseErr := url.Parse(baseURL)
	if parseErr != nil {
		return "", false
	}

	hostname := strings.ToLower(strings.TrimSpace(base.Hostname()))
	if hostname != "gitea" {
		return "", false
	}

	lowerErr := strings.ToLower(err.Error())
	if !strings.Contains(lowerErr, "lookup "+hostname) || !strings.Contains(lowerErr, "dial tcp") {
		return "", false
	}

	if port := base.Port(); port != "" {
		base.Host = net.JoinHostPort("localhost", port)
	} else {
		base.Host = "localhost"
	}
	return strings.TrimRight(base.String(), "/"), true
}
