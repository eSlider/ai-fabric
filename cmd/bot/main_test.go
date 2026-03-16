package main

import (
	"errors"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestSplitCommand(t *testing.T) {
	cmd, arg := splitCommand("/task implement x")
	if cmd != "/task" || arg != "implement x" {
		t.Fatalf("unexpected parse result: %s | %s", cmd, arg)
	}
}

func TestIsAllowed(t *testing.T) {
	cfg := config{
		AllowedChatIDs: parseSet("100"),
		AllowedUsers:   parseSet("alice"),
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
