package main

import (
	"testing"

	"produktor.io/ai-fabric/internal/config"
)

func TestLoadConfig_TransportSelectionAndCompatibility(t *testing.T) {
	t.Setenv("GITEA_BOT_BASE_URL", "http://gitea.local/")
	t.Setenv("GITEA_BOT_TOKEN", "secret_token")
	t.Setenv("ISSUE_POLL_INTERVAL_SEC", "30")
	t.Setenv("ISSUE_HANDLER_DRY_RUN", "true")
	t.Setenv("GITEA_CLI_DOCKER_NETWORK", "custom-net")
	t.Setenv("GITEA_TRANSPORT_PRIMARY", "sdk")
	t.Setenv("GITEA_CLI_FALLBACK_ENABLED", "true")

	cfg := config.LoadConfig()

	if cfg.Gitea.Token != "secret_token" {
		t.Errorf("expected secret_token, got %s", cfg.Gitea.Token)
	}
	if cfg.Issue.IssueBot.PollInterval != 30 {
		t.Errorf("expected 30, got %v", cfg.Issue.IssueBot.PollInterval)
	}
	if !cfg.Issue.DryRun {
		t.Errorf("expected DryRun to be true")
	}
	if cfg.Gitea.CLI.Docker.Network != "custom-net" {
		t.Errorf("expected custom-net, got %s", cfg.Gitea.CLI.Docker.Network)
	}
	if cfg.Gitea.PrimaryTransport != "sdk" {
		t.Errorf("expected sdk primary transport, got %s", cfg.Gitea.PrimaryTransport)
	}
	if !cfg.Gitea.CLIFallbackEnabled {
		t.Errorf("expected CLI fallback to be enabled")
	}
	if cfg.Gitea.CLI.URL != "http://gitea.local/" {
		t.Errorf("expected CLI URL from base URL, got %s", cfg.Gitea.CLI.URL)
	}
	if cfg.Gitea.CLI.Token != "secret_token" {
		t.Errorf("expected CLI token fallback to bot token, got %s", cfg.Gitea.CLI.Token)
	}
}
