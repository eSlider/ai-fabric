package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestHelpTextMentionsEveryMenuCommand(t *testing.T) {
	for _, cmd := range []string{
		"/menu", "/status", "/health", "/projects", "/task",
		"/checks", "/up", "/down", "/logs", "/help",
	} {
		if !strings.Contains(helpText(), cmd) {
			t.Fatalf("helpText() missing command %s", cmd)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	cmd, arg := splitCommand("/task implement x")
	if cmd != "/task" || arg != "implement x" {
		t.Fatalf("unexpected parse result: %s | %s", cmd, arg)
	}
}

func TestParseMCPToolRequestWithoutArgs(t *testing.T) {
	tool, args, err := parseMCPToolRequest("list_my_repos")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tool != "list_my_repos" {
		t.Fatalf("unexpected tool name: %s", tool)
	}
	if len(args) != 0 {
		t.Fatalf("expected empty args, got: %#v", args)
	}
}

func TestParseMCPToolRequestWithJSONArgs(t *testing.T) {
	tool, args, err := parseMCPToolRequest(`search_repos {"q":"ai-fabric"}`)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tool != "search_repos" {
		t.Fatalf("unexpected tool name: %s", tool)
	}
	if args["q"] != "ai-fabric" {
		t.Fatalf("unexpected args content: %#v", args)
	}
}

func TestParseMCPToolRequestRejectsNonJSONArgs(t *testing.T) {
	_, _, err := parseMCPToolRequest("search_repos q=ai-fabric")
	if err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestParseMCPToolRequestRejectsChatLikeText(t *testing.T) {
	_, _, err := parseMCPToolRequest("Hey")
	if err == nil {
		t.Fatalf("expected parse error for chat-like text")
	}
}

// API tests against a real HTTP server (no mocks): listProjects exercises the
// actual Gitea REST endpoints and fallback behavior.
func TestListProjectsFallsBackFromOrgToUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/eslider/repos":
			http.Error(w, `{"errors":["user redirect does not exist [name: eslider]"],"message":"GetOrgByName"}`, http.StatusNotFound)
			return
		case "/api/v1/users/eslider/repos":
			_, _ = w.Write([]byte(`[{"name":"ai-fabric","html_url":"http://example/ai-fabric"}]`))
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config{
		GiteaBaseURL:     server.URL,
		GiteaOwner:       "eslider",
		GiteaToken:       "token",
		ProjectListLimit: 20,
	}

	msg, err := listProjects(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "ai-fabric") {
		t.Fatalf("expected project in response, got: %s", msg)
	}
}

func TestListProjectsGeneratesURLWhenHTMLURLMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/eslider/repos":
			_, _ = w.Write([]byte(`[{"name":"ai-fabric","html_url":""}]`))
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config{
		GiteaBaseURL:     server.URL,
		GiteaOwner:       "eslider",
		GiteaToken:       "token",
		ProjectListLimit: 20,
	}

	msg, err := listProjects(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, server.URL+"/eslider/ai-fabric") {
		t.Fatalf("expected generated repo url, got: %s", msg)
	}
}

func TestIsAllowed(t *testing.T) {
	cfg := config{
		AllowedChatIDs: map[string]bool{"100": true},
		AllowedUsers:   map[string]bool{"alice": true},
	}
	msg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 100},
		From: &tgbotapi.User{UserName: "alice"},
	}
	if !isAllowed(cfg, msg) {
		t.Fatalf("expected allowed")
	}
}

func TestFallbackBaseURLForDNSError(t *testing.T) {
	err := errors.New(`Get "http://gitea:3000/api/v1/orgs/eslider/repos?limit=20": dial tcp: lookup gitea on 127.0.0.53:53: server misbehaving`)
	got, ok := fallbackBaseURLForDNSError("http://gitea:3000", err)
	if !ok {
		t.Fatalf("expected fallback to be enabled")
	}
	if got != "http://localhost:3000" {
		t.Fatalf("unexpected fallback base URL: %s", got)
	}
}

func TestFallbackBaseURLForDNSErrorSkipsNonGiteaHost(t *testing.T) {
	err := errors.New(`Get "http://example.com/api/healthz": dial tcp: lookup example.com: no such host`)
	_, ok := fallbackBaseURLForDNSError("http://example.com", err)
	if ok {
		t.Fatalf("expected fallback to be disabled for non-gitea host")
	}
}

func TestRouteFreeTextGreetingReturnsHelp(t *testing.T) {
	msg, err := routeFreeTextMessage(config{}, "Hi", 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(msg, "MCP request failed") {
		t.Fatalf("should not contain MCP error: %s", msg)
	}
	if !strings.Contains(msg, "/task") || !strings.Contains(msg, "/projects") || !strings.Contains(msg, "tools") {
		t.Fatalf("expected help mentioning /task, /projects, and tools, got: %s", msg)
	}
}

func TestRouteFreeTextTaskRequestCreatesIssue(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/eslider/ai-fabric/issues" {
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
			_, _ = w.Write([]byte(`{"id":1,"number":42,"html_url":"http://example/issues/42"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := config{
		GiteaBaseURL: server.URL,
		GiteaOwner:   "eslider",
		GiteaRepo:    "ai-fabric",
		GiteaToken:   "token",
	}
	chatID := int64(999)
	request := "нужно исправить ошибку в телеграм боте"

	msg, err := routeFreeTextMessage(cfg, request, chatID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "#42") || !strings.Contains(msg, "http://example/issues/42") {
		t.Fatalf("expected issue number and URL in reply, got: %s", msg)
	}
	if !strings.Contains(gotBody, request) {
		t.Fatalf("expected issue body to contain request text, got: %s", gotBody)
	}
	if !strings.Contains(gotBody, "<!-- ai-fabric:telegram-chat-id:999 -->") {
		t.Fatalf("expected telegram chat id marker in body, got: %s", gotBody)
	}
}

func TestRouteFreeTextEnglishTaskRequestCreatesIssue(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/eslider/ai-fabric/issues" {
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
			_, _ = w.Write([]byte(`{"id":1,"number":43,"html_url":"http://example/issues/43"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := config{
		GiteaBaseURL: server.URL,
		GiteaOwner:   "eslider",
		GiteaRepo:    "ai-fabric",
		GiteaToken:   "token",
	}
	chatID := int64(1001)
	request := "please fix the bot"

	msg, err := routeFreeTextMessage(cfg, request, chatID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "#43") || !strings.Contains(msg, "http://example/issues/43") {
		t.Fatalf("expected issue number and URL in reply, got: %s", msg)
	}
	if !strings.Contains(gotBody, request) {
		t.Fatalf("expected issue body to contain request text, got: %s", gotBody)
	}
	if !strings.Contains(gotBody, "<!-- ai-fabric:telegram-chat-id:1001 -->") {
		t.Fatalf("expected telegram chat id marker in body, got: %s", gotBody)
	}
}

func TestRouteFreeTextMCPListRepos(t *testing.T) {
	mcpServer := newTestMCPServer(t, func(toolName string, _ map[string]any) string {
		if toolName != "list_my_repos" {
			t.Fatalf("unexpected tool: %s", toolName)
		}
		return "repo-a\nrepo-b"
	})
	defer mcpServer.Close()

	cfg := config{MCPBaseURL: mcpServer.URL}
	msg, err := routeFreeTextMessage(cfg, "list_my_repos", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "repo-a") {
		t.Fatalf("expected MCP tool output, got: %s", msg)
	}
}

func TestRouteFreeTextMCPSearchRepos(t *testing.T) {
	mcpServer := newTestMCPServer(t, func(toolName string, args map[string]any) string {
		if toolName != "search_repos" {
			t.Fatalf("unexpected tool: %s", toolName)
		}
		if args["q"] != "ai-fabric" {
			t.Fatalf("unexpected args: %#v", args)
		}
		return "found: ai-fabric"
	})
	defer mcpServer.Close()

	cfg := config{MCPBaseURL: mcpServer.URL}
	msg, err := routeFreeTextMessage(cfg, `search_repos {"q":"ai-fabric"}`, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "ai-fabric") {
		t.Fatalf("expected search result, got: %s", msg)
	}
}

func newTestMCPServer(t *testing.T, onToolCall func(toolName string, args map[string]any) string) *httptest.Server {
	t.Helper()
	const sessionID = "test-session"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcpRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", sessionID)
			_ = json.NewEncoder(w).Encode(mcpRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{}`),
			})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(mcpRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"tools":[{"name":"list_my_repos"}]}`),
			})
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			raw, _ := json.Marshal(req.Params)
			if err := json.Unmarshal(raw, &params); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			text := onToolCall(params.Name, params.Arguments)
			result, _ := json.Marshal(map[string]any{
				"content": []map[string]string{{"type": "text", "text": text}},
			})
			_ = json.NewEncoder(w).Encode(mcpRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  result,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}
