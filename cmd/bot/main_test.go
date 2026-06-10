package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
