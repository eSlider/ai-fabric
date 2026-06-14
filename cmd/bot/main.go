package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	appconfig "example.org/ai-fabric/internal/config"
	"example.org/ai-fabric/pkg/gitea"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type config = appconfig.BotConfig

func main() {
	cfg := appconfig.LoadBotConfig()
	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}

	// Initialize Telegram bot
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
		if !strings.HasPrefix(text, "/") {
			msg, err := routeFreeTextMessage(cfg, text, update.Message.Chat.ID)
			if err != nil {
				msg = formatRouteError(err)
			}
			_ = reply(bot, update.Message.Chat.ID, trimLen(msg, 3900))
			continue
		}

		cmd, arg := splitCommand(text)
		switch cmd {
		case "/menu":
			_ = replyMenu(bot, update.Message.Chat.ID)
		case "/help":
			_ = reply(bot, update.Message.Chat.ID, helpText())
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
			_ = reply(bot, update.Message.Chat.ID, runScript("bin/up.sh", 180*time.Second, "--no-build"))
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
			_ = reply(bot, update.Message.Chat.ID, "Unknown command. Use /menu to open command buttons.")
		}
	}
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

func replyMenu(bot *tgbotapi.BotAPI, chatID int64) error {
	msg := tgbotapi.NewMessage(chatID, "Choose an action:")
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("/status"),
			tgbotapi.NewKeyboardButton("/health"),
			tgbotapi.NewKeyboardButton("/projects"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("/checks"),
			tgbotapi.NewKeyboardButton("/up"),
			tgbotapi.NewKeyboardButton("/down"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("/logs"),
			tgbotapi.NewKeyboardButton("/task"),
			tgbotapi.NewKeyboardButton("/help"),
		),
	)
	keyboard.ResizeKeyboard = true
	keyboard.OneTimeKeyboard = true
	keyboard.InputFieldPlaceholder = "Tap a command or type it"
	msg.ReplyMarkup = keyboard
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
		if fallbackBaseURL, ok := gitea.FallbackBaseURLForDNSError(cfg.GiteaBaseURL, err); ok {
			resp, err = http.Get(strings.Replace(healthURL, cfg.GiteaBaseURL, fallbackBaseURL, 1))
		}
	}
	if err != nil {
		return "Health check failed: " + err.Error()
	}
	defer resp.Body.Close()
	return fmt.Sprintf("Gitea health status: %d", resp.StatusCode)
}

func runScript(path string, timeout time.Duration, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmdArgs := append([]string{path}, args...)
	cmd := exec.CommandContext(ctx, "bash", cmdArgs...)
	out, err := cmd.CombinedOutput()
	label := path
	if len(args) > 0 {
		label += " " + strings.Join(args, " ")
	}
	if err != nil {
		return fmt.Sprintf("%s failed: %v\n%s", label, err, string(out))
	}
	return fmt.Sprintf("%s ok\n%s", label, string(out))
}

func runComposeLogs(service string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "bin/compose.sh", "-f", "docker-compose.yml", "logs", "--tail", "80", service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("logs failed: %v\n%s", err, string(out))
	}
	return string(out)
}

func giteaConfig(cfg config) gitea.BotConfig {
	return gitea.BotConfig{
		BaseURL: cfg.GiteaBaseURL,
		Owner:   cfg.GiteaOwner,
		Repo:    cfg.GiteaRepo,
		Token:   cfg.GiteaToken,
	}
}

func listProjects(cfg config) (string, error) {
	if cfg.GiteaBaseURL == "" || cfg.GiteaToken == "" || cfg.GiteaOwner == "" {
		return "", fmt.Errorf("gitea project variables are not fully configured")
	}

	svc := gitea.NewService(giteaConfig(cfg))
	repos, err := svc.ListOwnerRepos(cfg.GiteaOwner, cfg.ProjectListLimit)
	if err != nil {
		return "", err
	}
	if len(repos) == 0 {
		return "No projects found.", nil
	}

	baseURL := strings.TrimRight(svc.BaseURL(), "/")
	var b strings.Builder
	b.WriteString("Projects:\n")
	for _, r := range repos {
		b.WriteString("- " + r.Name)
		repoURL := strings.TrimSpace(r.HTMLURL)
		if repoURL == "" {
			repoURL = fmt.Sprintf("%s/%s/%s", baseURL, url.PathEscape(cfg.GiteaOwner), url.PathEscape(r.Name))
		}
		b.WriteString(" - " + repoURL)
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

	svc := gitea.NewService(giteaConfig(cfg))
	body := fmt.Sprintf("%s\n\n<!-- ai-fabric:telegram-chat-id:%d -->", description, chatID)
	issue, err := svc.CreateIssue(cfg.GiteaOwner, cfg.GiteaRepo, "[task] "+trimLen(description, 90), body)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created issue #%d\n%s", issue.Index, issue.HTMLURL), nil
}

type mcpRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *mcpRPCError    `json:"error"`
}

const minTaskRequestLen = 8

var taskRequestKeywords = []string{
	"сделай", "нужно", "исправь", "добавь", "реализуй", "создай", "почини",
	"fix", "add", "implement", "create", "need", "please", "should", "make",
}

func looksLikeMCPToolRequest(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	if strings.EqualFold(normalized, "tools") || strings.EqualFold(normalized, "mcp tools") {
		return true
	}
	parts := strings.SplitN(normalized, " ", 2)
	toolName := strings.TrimSpace(parts[0])
	if !isValidMCPToolName(toolName) {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(parts[1]), "{")
}

func looksLikeTaskRequest(text string) bool {
	normalized := strings.TrimSpace(text)
	if len(normalized) < minTaskRequestLen {
		return false
	}
	lower := strings.ToLower(normalized)
	for _, kw := range taskRequestKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return strings.Contains(normalized, "?")
}

func routeFreeTextMessage(cfg config, text string, chatID int64) (string, error) {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return freeTextHelpMessage(), nil
	}
	if looksLikeMCPToolRequest(normalized) {
		return routeMCPMessage(cfg, normalized)
	}
	if looksLikeTaskRequest(normalized) {
		return createTaskIssue(cfg, normalized, chatID)
	}
	return freeTextHelpMessage(), nil
}

func formatRouteError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "I can call MCP tools") || strings.Contains(msg, "MCP chat mode supports") {
		return msg
	}
	return "Request failed: " + msg
}

func freeTextHelpMessage() string {
	return strings.TrimSpace(`I did not recognize that message.

Send a task in natural language (e.g. "нужно исправить баг") or use:
- /task <description> — create a Gitea issue
- /projects — list projects
- tools — list MCP tools
- <tool-name> {"arg":"value"} — call an MCP tool`)
}

func routeMCPMessage(cfg config, text string) (string, error) {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return "", fmt.Errorf("empty MCP request")
	}
	if strings.EqualFold(normalized, "mcp tools") || strings.EqualFold(normalized, "tools") {
		return listMCPTools(cfg)
	}

	toolName, args, err := parseMCPToolRequest(normalized)
	if err != nil {
		return "", err
	}
	return callMCPTool(cfg, toolName, args)
}

func parseMCPToolRequest(text string) (string, map[string]any, error) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	toolName := strings.TrimSpace(parts[0])
	if toolName == "" {
		return "", nil, fmt.Errorf("tool name is required")
	}
	if !isValidMCPToolName(toolName) {
		return "", nil, fmt.Errorf("MCP chat mode supports tool calls only.\n\n%s", mcpUsageMessage(""))
	}

	if len(parts) == 1 {
		return toolName, map[string]any{}, nil
	}

	rawArgs := strings.TrimSpace(parts[1])
	if rawArgs == "" {
		return toolName, map[string]any{}, nil
	}
	if !strings.HasPrefix(rawArgs, "{") {
		return "", nil, fmt.Errorf("arguments must be a JSON object")
	}

	args := map[string]any{}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", nil, fmt.Errorf("invalid JSON args: %w", err)
	}
	return toolName, args, nil
}

func mcpInitParams() map[string]any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "ai-fabric-tg-bot",
			"version": "0.1.0",
		},
	}
}

func mcpSession(cfg config) (string, error) {
	_, httpResp, err := mcpRPC(cfg, 1, "initialize", mcpInitParams(), "")
	if err != nil {
		return "", err
	}
	return httpResp.Header.Get("Mcp-Session-Id"), nil
}

func listMCPTools(cfg config) (string, error) {
	sessionID, err := mcpSession(cfg)
	if err != nil {
		return "", err
	}

	toolsResp, _, err := mcpRPC(cfg, 2, "tools/list", map[string]any{}, sessionID)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(toolsResp.Result, &parsed); err != nil {
		return "", fmt.Errorf("failed to decode tools list: %w", err)
	}
	if len(parsed.Tools) == 0 {
		return "MCP tools list is empty.", nil
	}

	var b strings.Builder
	b.WriteString("MCP tools:\n")
	for i, tool := range parsed.Tools {
		if i >= 40 {
			b.WriteString("- ...\n")
			break
		}
		b.WriteString("- " + tool.Name)
		if tool.Description != "" {
			b.WriteString(": " + tool.Description)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func callMCPTool(cfg config, toolName string, args map[string]any) (string, error) {
	sessionID, err := mcpSession(cfg)
	if err != nil {
		return "", err
	}

	toolResp, _, err := mcpRPC(cfg, 2, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	}, sessionID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "tool not found") {
			return mcpUsageMessage(toolName), nil
		}
		return "", err
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool           `json:"isError"`
		Extra   map[string]any `json:"-"`
	}
	if err := json.Unmarshal(toolResp.Result, &parsed); err != nil {
		return "", fmt.Errorf("failed to decode tool response: %w", err)
	}

	var out strings.Builder
	for _, c := range parsed.Content {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(c.Text)
	}
	if out.Len() > 0 {
		return out.String(), nil
	}

	raw, err := json.MarshalIndent(toolResp.Result, "", "  ")
	if err != nil {
		return "MCP tool call completed.", nil
	}
	return string(raw), nil
}

func mcpRPC(cfg config, id int64, method string, params any, sessionID string) (mcpRPCResponse, *http.Response, error) {
	if cfg.MCPBaseURL == "" {
		return mcpRPCResponse{}, nil, fmt.Errorf("GITEA_MCP_BASE_URL is not configured")
	}

	payload := mcpRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return mcpRPCResponse{}, nil, err
	}

	req, err := http.NewRequest(http.MethodPost, cfg.MCPBaseURL, bytes.NewReader(data))
	if err != nil {
		return mcpRPCResponse{}, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	if cfg.MCPAccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.MCPAccessToken)
	}

	client := &http.Client{Timeout: 45 * time.Second}
	httpResp, err := client.Do(req)
	if err != nil {
		return mcpRPCResponse{}, nil, err
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return mcpRPCResponse{}, httpResp, err
	}
	if httpResp.StatusCode >= 400 {
		return mcpRPCResponse{}, httpResp, fmt.Errorf("mcp http error %d: %s", httpResp.StatusCode, string(body))
	}

	var parsed mcpRPCResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return mcpRPCResponse{}, httpResp, fmt.Errorf("failed to decode mcp response: %w", err)
	}
	if parsed.Error != nil {
		return mcpRPCResponse{}, httpResp, fmt.Errorf("mcp rpc error %d: %s", parsed.Error.Code, parsed.Error.Message)
	}
	return parsed, httpResp, nil
}

func isValidMCPToolName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isDigit && r != '_' {
			return false
		}
	}
	return true
}

func helpText() string {
	return strings.TrimSpace(`Commands:
/menu — show command buttons
/status — bot running status
/health — Gitea health check
/projects — list Gitea projects
/task <description> — create a task issue
/checks — run fmt, lint, and test scripts
/up — start docker compose stack
/down — stop docker compose stack
/logs <service> — show docker compose logs
/help — show this message

Natural-language task requests (e.g. "нужно исправить баг") also create Gitea issues.
For MCP tools, send "tools" to list them, or <tool-name> {"arg":"value"} to call one.`)
}

func mcpUsageMessage(toolName string) string {
	var b strings.Builder
	b.WriteString("I can call MCP tools, but I cannot do free-text chat.\n")
	if strings.TrimSpace(toolName) != "" {
		b.WriteString("\nUnknown tool: ")
		b.WriteString(toolName)
		b.WriteString("\n")
	}
	b.WriteString("\nUse:\n- tools\n- <tool-name>\n- <tool-name> {\"arg\":\"value\"}\n\nExample:\nsearch_repos {\"q\":\"ai-fabric\"}")
	return b.String()
}
