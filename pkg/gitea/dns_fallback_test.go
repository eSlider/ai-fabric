package gitea

import (
	"errors"
	"testing"
)

func TestFallbackBaseURLForDNSError(t *testing.T) {
	err := errors.New(`Get "http://gitea:3000/api/v1/orgs/eslider/repos?limit=20": dial tcp: lookup gitea on 127.0.0.53:53: server misbehaving`)
	got, ok := FallbackBaseURLForDNSError("http://gitea:3000", err)
	if !ok {
		t.Fatal("expected fallback to be enabled")
	}
	if got != "http://localhost:3000" {
		t.Fatalf("unexpected fallback base URL: %s", got)
	}
}

func TestFallbackBaseURLForDNSErrorSkipsNonGiteaHost(t *testing.T) {
	err := errors.New(`Get "http://example.com/api/healthz": dial tcp: lookup example.com: no such host`)
	_, ok := FallbackBaseURLForDNSError("http://example.com", err)
	if ok {
		t.Fatal("expected fallback to be disabled for non-gitea host")
	}
}
