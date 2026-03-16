package config

import "testing"

func TestLoadBotConfig_Defaults(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_ALLOWED_CHAT_IDS", "")
	t.Setenv("TELEGRAM_ALLOWED_USERNAMES", "")
	t.Setenv("GITEA_BOT_BASE_URL", "")
	t.Setenv("GITEA_BOT_OWNER", "")
	t.Setenv("GITEA_BOT_REPO", "")
	t.Setenv("GITEA_BOT_TOKEN", "")
	t.Setenv("GITEA_MCP_BASE_URL", "")
	t.Setenv("GITEA_ACCESS_TOKEN", "")
	t.Setenv("PROJECT_LIST_LIMIT", "")

	cfg := LoadBotConfig()

	if cfg.Token != "" {
		t.Fatalf("unexpected telegram token: %q", cfg.Token)
	}
	if len(cfg.AllowedChatIDs) != 0 {
		t.Fatalf("unexpected allowed chat ids: %#v", cfg.AllowedChatIDs)
	}
	if len(cfg.AllowedUsers) != 0 {
		t.Fatalf("unexpected allowed users: %#v", cfg.AllowedUsers)
	}
	if cfg.GiteaBaseURL != "http://localhost:3000" {
		t.Fatalf("unexpected gitea base url: %q", cfg.GiteaBaseURL)
	}
	if cfg.GiteaOwner != "eslider" {
		t.Fatalf("unexpected gitea owner: %q", cfg.GiteaOwner)
	}
	if cfg.GiteaRepo != "ai-fabric" {
		t.Fatalf("unexpected gitea repo: %q", cfg.GiteaRepo)
	}
	if cfg.GiteaToken != "" {
		t.Fatalf("unexpected gitea token: %q", cfg.GiteaToken)
	}
	if cfg.MCPBaseURL != "http://localhost:8080/mcp" {
		t.Fatalf("unexpected mcp base url: %q", cfg.MCPBaseURL)
	}
	if cfg.MCPAccessToken != "" {
		t.Fatalf("unexpected mcp access token: %q", cfg.MCPAccessToken)
	}
	if cfg.ProjectListLimit != 20 {
		t.Fatalf("unexpected project list limit: %d", cfg.ProjectListLimit)
	}
}

func TestLoadBotConfig_FromEnvironment(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", " bot-token ")
	t.Setenv("TELEGRAM_ALLOWED_CHAT_IDS", "100, 200")
	t.Setenv("TELEGRAM_ALLOWED_USERNAMES", "Alice,BOB")
	t.Setenv("GITEA_BOT_BASE_URL", "http://localhost:3000/")
	t.Setenv("GITEA_BOT_OWNER", " eslider ")
	t.Setenv("GITEA_BOT_REPO", " ai-fabric ")
	t.Setenv("GITEA_BOT_TOKEN", " gitea-token ")
	t.Setenv("GITEA_MCP_BASE_URL", "http://localhost:8080/mcp/")
	t.Setenv("GITEA_ACCESS_TOKEN", " access-token ")
	t.Setenv("PROJECT_LIST_LIMIT", "42")

	cfg := LoadBotConfig()

	if cfg.Token != "bot-token" {
		t.Fatalf("unexpected telegram token: %q", cfg.Token)
	}
	if !cfg.AllowedChatIDs["100"] || !cfg.AllowedChatIDs["200"] {
		t.Fatalf("unexpected allowed chat ids: %#v", cfg.AllowedChatIDs)
	}
	if !cfg.AllowedUsers["alice"] || !cfg.AllowedUsers["bob"] {
		t.Fatalf("unexpected allowed users: %#v", cfg.AllowedUsers)
	}
	if cfg.GiteaBaseURL != "http://localhost:3000/" {
		t.Fatalf("unexpected gitea base url: %q", cfg.GiteaBaseURL)
	}
	if cfg.GiteaOwner != "eslider" {
		t.Fatalf("unexpected gitea owner: %q", cfg.GiteaOwner)
	}
	if cfg.GiteaRepo != "ai-fabric" {
		t.Fatalf("unexpected gitea repo: %q", cfg.GiteaRepo)
	}
	if cfg.GiteaToken != "gitea-token" {
		t.Fatalf("unexpected gitea token: %q", cfg.GiteaToken)
	}
	if cfg.MCPBaseURL != "http://localhost:8080/mcp/" {
		t.Fatalf("unexpected mcp base url: %q", cfg.MCPBaseURL)
	}
	if cfg.MCPAccessToken != "access-token" {
		t.Fatalf("unexpected mcp access token: %q", cfg.MCPAccessToken)
	}
	if cfg.ProjectListLimit != 42 {
		t.Fatalf("unexpected project list limit: %d", cfg.ProjectListLimit)
	}
}
