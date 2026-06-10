package main

import (
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

func TestListProjectsGeneratesURLWhenHTMLURLMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
		case "/api/v1/orgs/eslider/repos":
			_, _ = w.Write([]byte(`[{"name":"ai-fabric","html_url":""}]`))
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

func TestListProjectsUsesGiteaService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
		case "/api/v1/orgs/eslider/repos":
			http.Error(w, `{"message":"GetOrgByName"}`, http.StatusNotFound)
		case "/api/v1/users/eslider/repos":
			_, _ = w.Write([]byte(`[{"name":"ai-fabric","html_url":"http://example/ai-fabric"}]`))
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
