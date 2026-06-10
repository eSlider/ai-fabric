//go:build system

package fabric

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "code.gitea.io/sdk/gitea"

	"example.org/ai-fabric/internal/config"
)

// System use-case test: drives the full issue flow against the real local
// Gitea instance with a deterministic stub agent binary (a real process, not
// a mock). Covers: architect-first gate, developer implementation, PR
// creation, labels, the single YAML status comment, and loop prevention
// (terminal label short-circuit).
//
// Requirements: docker compose Gitea on GITEA_BOT_BASE_URL, GITEA_BOT_TOKEN
// set (plus optional role tokens). Run via: go test -tags system ./pkg/fabric
func TestIssueFlowEndToEnd(t *testing.T) {
	baseURL := envOr("GITEA_BOT_BASE_URL", "http://localhost:3000")
	botToken := os.Getenv("GITEA_BOT_TOKEN")
	if botToken == "" {
		t.Skip("GITEA_BOT_TOKEN not set")
	}

	admin, err := sdk.NewClient(baseURL, sdk.SetToken(botToken))
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	me, _, err := admin.GetMyUserInfo()
	if err != nil {
		t.Fatalf("gitea unreachable: %v", err)
	}

	repoName := fmt.Sprintf("fabric-systest-%d", time.Now().Unix())
	repo, _, err := admin.CreateRepo(sdk.CreateRepoOption{
		Name:          repoName,
		AutoInit:      true,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	owner := me.UserName
	t.Cleanup(func() { _, _ = admin.DeleteRepo(owner, repoName) })

	for _, role := range []string{"ai-developer", "ai-reviewer", "ai-architect"} {
		// Go 1.26: new(expr) creates an initialized pointer in place.
		_, err := admin.AddCollaborator(owner, repoName, role, sdk.AddCollaboratorOption{Permission: new(sdk.AccessModeWrite)})
		if err != nil {
			t.Logf("add collaborator %s: %v (role tokens may fall back to bot token)", role, err)
		}
	}

	workDir := t.TempDir()
	cloneDir := filepath.Join(workDir, "repo")
	authURL := strings.Replace(repo.CloneURL, "://", "://oauth2:"+botToken+"@", 1)
	mustRun(t, workDir, "git", "clone", authURL, cloneDir)
	mustRun(t, cloneDir, "git", "config", "user.name", "systest")
	mustRun(t, cloneDir, "git", "config", "user.email", "systest@ai-fabric.local")

	// Seed the check scripts the handler runs in worktrees.
	binDir := filepath.Join(cloneDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fmt.sh", "lint.sh", "test.sh"} {
		script := "#!/usr/bin/env bash\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, cloneDir, "git", "add", ".")
	mustRun(t, cloneDir, "git", "commit", "-m", "seed check scripts")
	mustRun(t, cloneDir, "git", "push", "origin", "main")

	stubAgent := writeStubAgent(t, workDir)

	cfg := config.LoadConfig()
	cfg.RootDir = cloneDir
	cfg.Gitea.BaseURL = baseURL
	cfg.Gitea.Owner = owner
	cfg.Gitea.Repo = repoName
	cfg.Issue.AgentBin = stubAgent
	cfg.Issue.BaseBranch = "main"
	cfg.Issue.DryRun = false
	cfg.Issue.TelegramBotToken = ""
	cfg.Issue.Architect.Enabled = true

	h := NewIssueHandler(cfg)

	issue, _, err := admin.CreateIssue(owner, repoName, sdk.CreateIssueOption{
		Title: "Add greeting feature",
		Body:  "Please add a greeting feature.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Cycle 1: architect-first gate — only the analysis is produced.
	loaded, err := h.Developer.GetIssue(owner, repoName, issue.Index)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ProcessIssue(loaded); err != nil {
		t.Fatalf("architect cycle: %v", err)
	}
	loaded, err = h.Developer.GetIssue(owner, repoName, issue.Index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loaded.Body, ArchEnd) {
		t.Fatalf("expected architect analysis in issue body, got:\n%s", loaded.Body)
	}
	if !strings.Contains(loaded.Body, "Recommended Approach") {
		t.Fatalf("expected structured analysis, got:\n%s", loaded.Body)
	}

	// Cycle 2: developer implements and opens a PR.
	if err := h.ProcessIssue(loaded); err != nil {
		t.Fatalf("developer cycle: %v", err)
	}

	prs, err := h.Developer.ListOpenPullRequests(owner, repoName)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected exactly one PR, got %d", len(prs))
	}
	if !strings.Contains(prs[0].Body, fmt.Sprintf("Closes #%d", issue.Index)) {
		t.Fatalf("PR body missing issue link:\n%s", prs[0].Body)
	}

	loaded, err = h.Developer.GetIssue(owner, repoName, issue.Index)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusLabel(loaded); got != "pr_opened" {
		t.Fatalf("expected status:pr_opened, got %q", got)
	}

	// Single status comment with machine-readable YAML state.
	comments, err := h.Developer.ListIssueComments(owner, repoName, issue.Index)
	if err != nil {
		t.Fatal(err)
	}
	statusComments := 0
	var state *workStatus
	for _, c := range comments {
		if s, ok := parseWorkStatus(c.Body); ok {
			statusComments++
			state = s
		}
	}
	if statusComments != 1 {
		t.Fatalf("expected exactly one status comment, got %d", statusComments)
	}
	if state.Stage != stagePROpened || state.Attempts != 1 || state.PRURL == "" {
		t.Fatalf("unexpected status state: %+v", state)
	}

	// Cycle 3: loop prevention — terminal label short-circuits, nothing changes.
	if err := h.ProcessIssue(loaded); err != nil {
		t.Fatalf("terminal cycle: %v", err)
	}
	prs, err = h.Developer.ListOpenPullRequests(owner, repoName)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("loop detected: expected one PR after reprocessing, got %d", len(prs))
	}
}

// writeStubAgent creates a deterministic agent binary: architect prompts get a
// structured analysis on stdout, developer prompts produce a file change.
func writeStubAgent(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "stub-agent")
	script := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--list-models" ]]; then
  echo "composer-test"
  exit 0
fi
workspace="."
args=("$@")
for ((i=0; i<${#args[@]}; i++)); do
  if [[ "${args[$i]}" == "--workspace" ]]; then
    workspace="${args[$((i+1))]}"
  fi
done
prompt="${args[${#args[@]}-1]}"
if [[ "$prompt" == *"Solution Architect analysis"* ]]; then
  cat <<'EOF'
## Possible Solutions
- Option A: implement greeting in a new file

## Recommended Approach
- Option A, minimal change

## Implementation Structure
- Add GREETING.md

## Estimation
- Complexity: S

## Required Skills/Context
- docs/skills/developer.md
EOF
  exit 0
fi
echo "greeting implemented" > "${workspace}/GREETING.md"
echo "done"
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
