package gitea

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newGiteaAPIServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
			return
		}
		if handler != nil {
			handler(w, r)
		}
	}))
}

func TestListOwnerReposFallsBackFromOrgToUser(t *testing.T) {
	server := newGiteaAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/eslider/repos":
			http.Error(w, `{"errors":["user redirect does not exist [name: eslider]"],"message":"GetOrgByName"}`, http.StatusNotFound)
		case "/api/v1/users/eslider/repos":
			_, _ = w.Write([]byte(`[{"name":"ai-fabric","html_url":"http://example/ai-fabric"}]`))
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	svc := NewService(BotConfig{
		BaseURL: server.URL,
		Token:   "token",
		Owner:   "eslider",
	})

	repos, err := svc.ListOwnerRepos("eslider", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "ai-fabric" {
		t.Fatalf("unexpected repos: %#v", repos)
	}
}

func TestListOwnerReposGeneratesURLWhenHTMLURLMissing(t *testing.T) {
	server := newGiteaAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orgs/eslider/repos" {
			_, _ = w.Write([]byte(`[{"name":"ai-fabric","html_url":""}]`))
			return
		}
		http.NotFound(w, r)
	})
	defer server.Close()

	svc := NewService(BotConfig{
		BaseURL: server.URL,
		Token:   "token",
		Owner:   "eslider",
	})

	repos, err := svc.ListOwnerRepos("eslider", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected one repo, got %#v", repos)
	}
	repoURL := strings.TrimSpace(repos[0].HTMLURL)
	if repoURL == "" {
		repoURL = strings.TrimRight(svc.BaseURL(), "/") + "/eslider/ai-fabric"
	}
	expected := strings.TrimRight(server.URL, "/") + "/eslider/ai-fabric"
	if repoURL != expected {
		t.Fatalf("expected generated repo url %q, got %q", expected, repoURL)
	}
}

func TestListOwnerReposDNSFallbackOnSDKInit(t *testing.T) {
	server := newGiteaAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orgs/eslider/repos" {
			_, _ = w.Write([]byte(`[{"name":"ai-fabric","html_url":"http://example/ai-fabric"}]`))
			return
		}
		http.NotFound(w, r)
	})
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	svc := NewService(BotConfig{
		BaseURL: "http://gitea:" + u.Port(),
		Token:   "token",
		Owner:   "eslider",
	})

	repos, err := svc.ListOwnerRepos("eslider", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.BaseURL() != "http://localhost:"+u.Port() {
		t.Fatalf("expected fallback base URL, got %s", svc.BaseURL())
	}
	if len(repos) != 1 || repos[0].Name != "ai-fabric" {
		t.Fatalf("unexpected repos: %#v", repos)
	}
}
