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

	appconfig "produktor.io/ai-fabric/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type config = appconfig.BotConfig

func main() {
	cfg := appconfig.LoadBotConfig()
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
		if !strings.HasPrefix(text, "/") {
			msg, err := routeMCPMessage(cfg, text)
			if err != nil {
				msg = "MCP request failed: " + err.Error() + "\n\nUse: <tool-name> {\"arg\":\"value\"}\nExample: list_my_repos"
			}
			_ = reply(bot, update.Message.Chat.ID, trimLen(msg, 3900))
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

	repos, err := listProjectsFromEndpoint(cfg, "orgs")
	if err != nil {
		// If owner is not an org, Gitea returns 404 GetOrgByName; fallback to user repos.
		if strings.Contains(strings.ToLower(err.Error()), "gitea error: 404") {
			repos, err = listProjectsFromEndpoint(cfg, "users")
		}
	}
	if err != nil {
		return "", err
	}
	if len(repos) == 0 {
		return "No projects found.", nil
	}

	var b strings.Builder
	b.WriteString("Projects:\n")
	for _, r := range repos {
		b.WriteString("- " + r.Name)
		repoURL := strings.TrimSpace(r.URL)
		if repoURL == "" {
			repoURL = fmt.Sprintf("%s/%s/%s", strings.TrimRight(cfg.GiteaBaseURL, "/"), url.PathEscape(cfg.GiteaOwner), url.PathEscape(r.Name))
		}
		b.WriteString(" - " + repoURL)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func listProjectsFromEndpoint(cfg config, ownerType string) ([]struct {
	Name string `json:"name"`
	URL  string `json:"html_url"`
}, error) {
	u := fmt.Sprintf("%s/api/v1/%s/%s/repos?limit=%d", cfg.GiteaBaseURL, ownerType, url.PathEscape(cfg.GiteaOwner), cfg.ProjectListLimit)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+cfg.GiteaToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if fallbackBaseURL, ok := fallbackBaseURLForDNSError(cfg.GiteaBaseURL, err); ok {
			u = strings.Replace(u, cfg.GiteaBaseURL, fallbackBaseURL, 1)
			req, err = http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "token "+cfg.GiteaToken)
			resp, err = http.DefaultClient.Do(req)
		}
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitea error: %d %s", resp.StatusCode, string(body))
	}

	var repos []struct {
		Name string `json:"name"`
		URL  string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, err
	}
	return repos, nil
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

func listMCPTools(cfg config) (string, error) {
	_, httpResp, err := mcpRPC(cfg, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "ai-fabric-tg-bot",
			"version": "0.1.0",
		},
	}, "")
	if err != nil {
		return "", err
	}

	sessionID := httpResp.Header.Get("Mcp-Session-Id")
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
	_, httpResp, err := mcpRPC(cfg, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "ai-fabric-tg-bot",
			"version": "0.1.0",
		},
	}, "")
	if err != nil {
		return "", err
	}

	sessionID := httpResp.Header.Get("Mcp-Session-Id")
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
