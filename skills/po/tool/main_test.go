package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"code.gitea.io/sdk/gitea"
)

// capture runs fn and returns what it wrote to stdout.
func capture(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	buf := make([]byte, 64*1024)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

// testClient returns a *gitea.Client wired to a fake server. Version is pinned
// so the SDK never hits /version during tests (offline).
func testClient(h http.Handler) *gitea.Client {
	srv := httptest.NewServer(h)
	c, err := gitea.NewClient(srv.URL, gitea.SetToken("test-token"), gitea.SetGiteaVersion("1.28.0"))
	if err != nil {
		panic(err)
	}
	return c
}

const fakeRepo = "<owner>/<project>"

// ---------------------------------------------------------------------------
// scanDiff — pure offline parser

const diffWithSecret = `diff --git a/config.yml b/config.yml
index 111..222 100644
--- a/config.yml
+++ b/config.yml
@@ -1,3 +1,3 @@
 host: example.com
-api_key: abc
+api_key: AKIAIOSFODNN7EXAMPLE
@@ -10,2 +10,3 @@
 context:
-  old: true
+  dir: /Users/user/work
`

func TestScanDiffSecretsAndPaths(t *testing.T) {
	hits := scanDiff([]byte(diffWithSecret))
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %d: %+v", len(hits), hits)
	}
	var cats []string
	for _, h := range hits {
		cats = append(cats, h.cat)
		if h.file != "config.yml" {
			t.Errorf("file = %q, want config.yml", h.file)
		}
		if strings.Contains(h.desc, "AKIA") || strings.Contains(h.desc, "/Users/user") {
			t.Errorf("secret/path value leaked in desc %q", h.desc)
		}
	}
	got := strings.Join(cats, ",")
	if !strings.Contains(got, "secret:aws-access-key") {
		t.Errorf("missing aws-access-key hit: %v", hits)
	}
	if !strings.Contains(got, "hardcoded-path") {
		t.Errorf("missing hardcoded-path hit: %v", hits)
	}
	// line numbers: secret on new-file line 2, path on new-file line 11
	if hits[0].line != 2 {
		t.Errorf("secret line = %d, want 2", hits[0].line)
	}
	if hits[1].line != 11 {
		t.Errorf("path line = %d, want 11", hits[1].line)
	}
}

func TestScanDiffGenericKeyMasksValue(t *testing.T) {
	d := "+++ b/a\n@@ -1,1 +1,1 @@\n+DB_PASSWORD = supersecret1234\n"
	hits := scanDiff([]byte(d))
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
	if hits[0].cat != "secret:generic-key" {
		t.Errorf("cat = %q, want secret:generic-key", hits[0].cat)
	}
	if strings.Contains(hits[0].desc, "supersecret1234") {
		t.Errorf("secret value leaked: %q", hits[0].desc)
	}
	if !strings.HasPrefix(hits[0].desc, "DB_PASSWORD=***") {
		t.Errorf("desc = %q, want DB_PASSWORD=***", hits[0].desc)
	}
}

func TestScanDiffClean(t *testing.T) {
	d := "+++ b/ok.go\n@@ -1,1 +1,1 @@\n+var x = 1\n"
	if hits := scanDiff([]byte(d)); len(hits) != 0 {
		t.Errorf("clean diff produced hits: %+v", hits)
	}
}

// ---------------------------------------------------------------------------
// httptest command tests

func TestCmdPR(t *testing.T) {
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/42"):
			w.Write([]byte(`{"number":42,"state":"open","title":"T","mergeable":true,"merged":false,
				"head":{"ref":"feat/x#1","sha":"abcdef0123456789"},"base":{"ref":"release/v1","sha":"abc"}}`))
		case strings.HasSuffix(r.URL.Path, "/pulls/42/files"):
			w.Write([]byte(`[{"filename":"main.go","status":"modified","additions":4,"deletions":1}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	out := capture(func() { cmdPR(c, "eSlider", "<project>", []string{"42"}) })
	for _, want := range []string{"[open]", "mergeable: yes", "feat/x#1", "abcdef01", "release/v1", "main.go", "+4 -1"} {
		if !strings.Contains(out, want) {
			t.Errorf("pr output missing %q:\n%s", want, out)
		}
	}
}

func TestCmdPRsMergeable(t *testing.T) {
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"number":7,"state":"open","title":"A","mergeable":true,"merged":false},
			{"number":8,"state":"open","title":"B","mergeable":false,"merged":false},
			{"number":9,"state":"closed","title":"C","mergeable":true,"merged":true}]`))
	}))
	out := capture(func() { cmdPRs(c, "eSlider", "<project>", []string{"open"}) })
	for _, want := range []string{"#7", "m=yes", "m=no", "#9", "m=-"} {
		if !strings.Contains(out, want) {
			t.Errorf("prs output missing %q:\n%s", want, out)
		}
	}
}

func TestCmdRun(t *testing.T) {
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total_count":2,"workflow_runs":[
			{"id":11,"run_number":5,"status":"in_progress","head_branch":"feat/x","head_sha":"aaaa","display_title":"T1","html_url":"http://x/r/11"},
			{"id":10,"run_number":4,"status":"completed","conclusion":"success","head_branch":"feat/x","head_sha":"bbbb","display_title":"T2","html_url":"http://x/r/10"}]}`))
	}))
	out := capture(func() { cmdRun(c, "eSlider", "<project>", []string{"feat/x"}) })
	for _, want := range []string{"in_progress", "success", "HEAD run 11"} {
		if !strings.Contains(out, want) {
			t.Errorf("run output missing %q:\n%s", want, out)
		}
	}
}

func TestCmdMerge(t *testing.T) {
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls/42/merge"):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/42"):
			w.Write([]byte(`{"number":42,"state":"closed","mergeable":true,"merged":true,
				"html_url":"http://x/pulls/42","head":{"ref":"feat/x","sha":"aaa"},"base":{"ref":"release/v1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	out := capture(func() { cmdMerge(c, "eSlider", "<project>", []string{"42"}) })
	for _, want := range []string{"merged #42", "merged: true", "http://x/pulls/42"} {
		if !strings.Contains(out, want) {
			t.Errorf("merge output missing %q: %q", want, out)
		}
	}
}

func TestCmdPRCreate(t *testing.T) {
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/pulls") {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Head, Base, Title, Body string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode create body: %v", err)
		}
		if req.Head != "feat/x#187" || req.Base != "release/v1" || req.Title != "T" || req.Body != "B" {
			t.Errorf("create PR body = %+v", req)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"number":60,"title":"T","html_url":"http://x/pulls/60"}`)
	}))
	out := capture(func() { cmdPRCreate(c, "eSlider", "<project>", []string{"feat/x#187", "release/v1", "T", "B"}) })
	for _, want := range []string{"created PR #60", "http://x/pulls/60"} {
		if !strings.Contains(out, want) {
			t.Errorf("pr-create output missing %q:\n%s", want, out)
		}
	}
}

func TestCmdIssueShow(t *testing.T) {
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/issues/9"):
			w.Write([]byte(`{"number":9,"state":"open","title":"T","body":"body text",
				"labels":[{"name":"epic"},{"name":"bug"}],
				"milestone":{"title":"sprint-1"}}`))
		case strings.HasSuffix(r.URL.Path, "/issues/9/comments"):
			w.Write([]byte(`[{"id":1,"body":"first","user":{"login":"alice"},"created_at":"2026-01-02T10:00:00Z"},
				{"id":2,"body":"second","user":{"login":"bob"}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	out := capture(func() { cmdIssueShow(c, "eSlider", "<project>", []string{"9"}) })
	for _, want := range []string{"[open]", "T", "body text", "epic, bug", "sprint-1", "alice", "first", "bob", "second"} {
		if !strings.Contains(out, want) {
			t.Errorf("issue-show output missing %q:\n%s", want, out)
		}
	}
}

func TestCmdJobLogs(t *testing.T) {
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/runs/9/jobs"):
			w.Write([]byte(`{"total_count":1,"jobs":[{"id":100,"run_id":9,"name":"Test","conclusion":"failure","steps":[
				{"name":"checkout","number":1,"conclusion":"success"},
				{"name":"go vet","number":2,"conclusion":"failure"}]}]}`))
		case strings.HasSuffix(r.URL.Path, "/jobs/100/logs"):
			w.Write([]byte("line1\nline2\n\x1b[31mfatal: boom\x1b[0m\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	out := capture(func() { cmdJobLogs(c, "eSlider", "<project>", []string{"9"}) })
	for _, want := range []string{"job 100 Test: FAILED", "> step 2 go vet", "fatal: boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("joblogs output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("ANSI codes not stripped:\n%q", out)
	}
}

func TestCmdJobLogsNoFailed(t *testing.T) {
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total_count":1,"jobs":[{"id":100,"run_id":9,"name":"Test","conclusion":"success","steps":[]}]}`))
	}))
	out := capture(func() { cmdJobLogs(c, "eSlider", "<project>", []string{"9"}) })
	if !strings.Contains(out, "no failed jobs") {
		t.Errorf("joblogs output = %q", out)
	}
}

func TestCmdIssueCreate(t *testing.T) {
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/issues") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"number":190,"title":"t"}`)
	}))
	out := capture(func() { cmdIssueCreate(c, "eSlider", "<project>", []string{"t", "b"}) })
	if !strings.Contains(out, "created #190") {
		t.Errorf("issue-create output = %q", out)
	}
}

func TestCmdScan(t *testing.T) {
	// compare returns one ahead commit; its .diff endpoint returns the payload.
	c := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/compare/"):
			w.Write([]byte(`{"total_commits":1,"commits":[{"sha":"deadbeefdeadbeef"}]}`))
		case strings.HasSuffix(r.URL.Path, "/git/commits/deadbeefdeadbeef.diff"):
			w.Write([]byte(diffWithSecret))
		default:
			http.NotFound(w, r)
		}
	}))
	out := capture(func() { cmdScan(c, "eSlider", "<project>", []string{"feat/x", "release/v1"}) })
	for _, want := range []string{"2 hit(s)", "secret:aws-access-key", "config.yml:2", "config.yml:11"} {
		if !strings.Contains(out, want) {
			t.Errorf("scan output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "AKIA") {
		t.Errorf("secret value leaked in scan output:\n%s", out)
	}
}
