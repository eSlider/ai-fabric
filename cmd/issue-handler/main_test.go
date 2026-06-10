package main

import (
	"testing"

	"example.org/ai-fabric/internal/config"
)

// Use-case test: the process reads its configuration from real environment
// variables; verifies role token fallbacks and tunables end-to-end.
func TestLoadConfigFromEnvironment(t *testing.T) {
	t.Setenv("GITEA_BOT_BASE_URL", "http://gitea.local/")
	t.Setenv("GITEA_BOT_TOKEN", "secret_token")
	t.Setenv("GITEA_HANDLER_TOKEN", "")
	t.Setenv("GITEA_REVIEWER_TOKEN", "reviewer_token")
	t.Setenv("GITEA_ARCHITECT_TOKEN", "")
	t.Setenv("GITEA_WEBHOOK_SECRET", "hook_secret")
	t.Setenv("ISSUE_POLL_INTERVAL_SEC", "30")
	t.Setenv("ISSUE_IN_PROGRESS_TIMEOUT_SEC", "120")
	t.Setenv("ISSUE_HANDLER_DRY_RUN", "true")

	cfg := config.LoadConfig()

	if cfg.Gitea.Token != "secret_token" {
		t.Errorf("expected secret_token, got %s", cfg.Gitea.Token)
	}
	if cfg.Gitea.HandlerToken != "secret_token" {
		t.Errorf("expected handler token fallback to bot token, got %s", cfg.Gitea.HandlerToken)
	}
	if cfg.Gitea.ReviewerToken != "reviewer_token" {
		t.Errorf("expected reviewer_token, got %s", cfg.Gitea.ReviewerToken)
	}
	if cfg.Gitea.ArchitectToken != "secret_token" {
		t.Errorf("expected architect token fallback to bot token, got %s", cfg.Gitea.ArchitectToken)
	}
	if cfg.Issue.Webhook.Secret != "hook_secret" {
		t.Errorf("expected hook_secret, got %s", cfg.Issue.Webhook.Secret)
	}
	if cfg.Issue.IssueBot.PollInterval != 30 {
		t.Errorf("expected 30, got %v", cfg.Issue.IssueBot.PollInterval)
	}
	if cfg.Issue.InProgressTimeoutSec != 120 {
		t.Errorf("expected 120, got %v", cfg.Issue.InProgressTimeoutSec)
	}
	if !cfg.Issue.DryRun {
		t.Errorf("expected DryRun to be true")
	}
	if cfg.Issue.Webhook.CIFixMaxPerSHA != 2 || cfg.Issue.Webhook.CIFixMaxPerPR != 6 {
		t.Errorf("unexpected CI fix budgets: %+v", cfg.Issue.Webhook)
	}
}
