package fabric

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.org/ai-fabric/internal/config"
)

// API tests: real HTTP against the webhook endpoint (no mocks). Only paths
// that terminate before any Gitea call are covered here; full event flows are
// exercised by the system tests (//go:build system).
func newWebhookServer(secret string) *httptest.Server {
	cfg := &config.Config{}
	cfg.Issue.Webhook.Secret = secret
	h := NewIssueHandler(cfg)
	return httptest.NewServer(http.HandlerFunc(h.ServeWebhook))
}

func TestWebhookRejectsNonPost(t *testing.T) {
	server := newWebhookServer("")
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestWebhookRejectsInvalidSignature(t *testing.T) {
	server := newWebhookServer("topsecret")
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{}`))
	req.Header.Set("X-Gitea-Event", "push")
	req.Header.Set("X-Gitea-Signature", "deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWebhookAcceptsValidSignatureForUnhandledEvent(t *testing.T) {
	secret := "topsecret"
	server := newWebhookServer(secret)
	defer server.Close()

	body := `{"action":"created"}`
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))

	req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(body))
	req.Header.Set("X-Gitea-Event", "push")
	req.Header.Set("X-Gitea-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
