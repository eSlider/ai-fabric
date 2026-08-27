// Command po is the PO assistant for Gitea (<project> & co). Uses the Gitea SDK
// (code.gitea.io/sdk/gitea) directly — no shell/jq/yq/python wrapper.
//
// Config: token + host are read from the tea config (~/.tea/tea.yml), same login
// the `tea` CLI uses. Override with PO_REPO / PO_TEA_YML env.
//
// Usage:
//
//	po epics                      epic status report (children via body refs)
//	po issues [state] [-k kw] [-L label] [-m milestone]
//	po issue-create "<title>" "<body>"
//	po issue-show <n>             issue body + state + labels + milestone + comments
//	po pr-create <head> <base> "<title>" "<body>"
//	po comment <index> "<body>"
//	po close <index> [index...]
//	po reopen <index> [index...]
//	po milestone [all|open|closed]
//	po prs [state]                PR list with mergeable column
//	po pr <n>                     PR detail: state, mergeable, refs, sha, files
//	po run <branch>               CI workflow-runs for a head branch
//	po merge <n>                  merge a PR (Do=merge, delete branch)
//	po joblogs <run>              failed job step + log tail for a run
//	po scan <branch> [base]       scan branch diff for secrets / hardcoded paths
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"code.gitea.io/sdk/gitea"
	"gopkg.in/yaml.v3"
)

const defLogin = "gitea"
const defRepo = "<owner>/<project>"
const defBase = "release/v1"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	cfg := loadCfg()
	c, err := gitea.NewClient(cfg.URL, gitea.SetToken(cfg.Token))
	if err != nil {
		fatal("client: %v", err)
	}
	owner, name := splitRepo(cfg.Repo)

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "epics":
		cmdEpics(c, owner, name)
	case "issues", "ls":
		cmdIssues(c, owner, name, rest)
	case "issue-create":
		cmdIssueCreate(c, owner, name, rest)
	case "issue-show", "show":
		cmdIssueShow(c, owner, name, rest)
	case "pr-create":
		cmdPRCreate(c, owner, name, rest)
	case "comment":
		cmdComment(c, owner, name, rest)
	case "close":
		cmdState(c, owner, name, "closed", rest)
	case "reopen":
		cmdState(c, owner, name, "open", rest)
	case "milestone", "ms":
		cmdMilestones(c, owner, name, rest)
	case "prs":
		cmdPRs(c, owner, name, rest)
	case "pr":
		cmdPR(c, owner, name, rest)
	case "run":
		cmdRun(c, owner, name, rest)
	case "merge":
		cmdMerge(c, owner, name, rest)
	case "joblogs":
		cmdJobLogs(c, owner, name, rest)
	case "scan":
		cmdScan(c, owner, name, rest)
	case "-h", "--help", "help":
		usage()
	default:
		fatal("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `po — PO assistant for Gitea (Gitea SDK, not shell).

Usage:
  po epics                      epic status report
  po issues [state] [-k kw] [-L label] [-m milestone] [-A author]
  po issue-create "<title>" "<body>"
  po issue-show <n>             issue body + state + labels + milestone + comments
  po pr-create <head> <base> "<title>" "<body>"
  po comment <index> "<body>"
  po close <index> [index...]
  po reopen <index> [index...]
  po milestone [all|open|closed]
  po prs [state]                PR list with mergeable column
  po pr <n>                     PR detail (state, mergeable, refs, sha, files)
  po run <branch>               CI workflow-runs for a head branch
  po merge <n>                  merge a PR (Do=merge, delete branch)
  po joblogs <run>              failed job step + log tail for a run
  po scan <branch> [base]       scan branch diff for secrets / hardcoded paths

Env: PO_REPO (owner/repo, default <owner>/<project>), PO_LOGIN (tea login, default gitea),
     PO_TEA_YML (tea config path, default ~/.tea/tea.yml),
     PO_BASE (scan base branch, default release/v1).
`)
}

// ---------------------------------------------------------------------------
// config

type cfg struct {
	URL, Token, Repo string
}

func loadCfg() cfg {
	login := envOr("PO_LOGIN", defLogin)
	repo := envOr("PO_REPO", defRepo)
	yml := envOr("PO_TEA_YML", filepath.Join(home(), ".tea", "tea.yml"))
	raw, err := os.ReadFile(yml)
	if err != nil {
		fatal("read tea config %s: %v", yml, err)
	}
	var doc struct {
		Logins []struct {
			Name  string `yaml:"name"`
			URL   string `yaml:"url"`
			Token string `yaml:"token"`
		} `yaml:"logins"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		fatal("parse tea config: %v", err)
	}
	for _, l := range doc.Logins {
		if l.Name == login {
			return cfg{URL: l.URL, Token: l.Token, Repo: repo}
		}
	}
	fatal("login %q not found in %s", login, yml)
	return cfg{}
}

func home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func splitRepo(repo string) (string, string) {
	p := strings.SplitN(repo, "/", 2)
	if len(p) != 2 {
		fatal("bad repo %q (want owner/name)", repo)
	}
	return p[0], p[1]
}

// ---------------------------------------------------------------------------
// helpers

func listAllIssues(c *gitea.Client, owner, name string, state gitea.StateType) []*gitea.Issue {
	var out []*gitea.Issue
	page := 1
	for {
		is, _, err := c.ListRepoIssues(owner, name, gitea.ListIssueOption{
			ListOptions: gitea.ListOptions{Page: page, PageSize: 50},
			State:       state,
			Type:        gitea.IssueTypeIssue,
		})
		if err != nil {
			fatal("list issues: %v", err)
		}
		if len(is) == 0 {
			break
		}
		out = append(out, is...)
		if len(is) < 50 {
			break
		}
		page++
	}
	return out
}

func isEpic(i *gitea.Issue) bool {
	for _, l := range i.Labels {
		if l.Name == "epic" {
			return true
		}
	}
	return false
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "po: "+f+"\n", a...)
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// epics

func cmdEpics(c *gitea.Client, owner, name string) {
	all := listAllIssues(c, owner, name, gitea.StateAll)
	sort.Slice(all, func(i, j int) bool { return all[i].Index < all[j].Index })

	var epics []*gitea.Issue
	for _, i := range all {
		if i.State == gitea.StateOpen && isEpic(i) {
			epics = append(epics, i)
		}
	}
	if len(epics) == 0 {
		fmt.Println("no open epics")
		return
	}

	epicRe := map[int64]*regexp.Regexp{}
	for _, e := range epics {
		n := e.Index
		// patterns: "Epic: #N", "Родитель: #N", "epic ... #N"
		epicRe[n] = regexp.MustCompile(`(?:Epic|epic|эпик|Родител[ьи])[^\n#]*#` + strconv.FormatInt(n, 10) + `\b`)
	}

	for _, e := range epics {
		var kids []*gitea.Issue
		re := epicRe[e.Index]
		for _, i := range all {
			if i.Index == e.Index {
				continue
			}
			if re.MatchString(i.Body + " " + i.Title) {
				kids = append(kids, i)
			}
		}
		var closed, open []string
		for _, k := range kids {
			if k.State == gitea.StateClosed {
				closed = append(closed, strconv.FormatInt(k.Index, 10))
			} else {
				open = append(open, fmt.Sprintf("#%d %s", k.Index, k.Title))
			}
		}
		fmt.Printf("=== #%d: %s\n", e.Index, e.Title)
		fmt.Printf("  children: %d  closed: [%s]\n", len(kids), strings.Join(closed, ","))
		if len(open) > 0 {
			for _, o := range open {
				fmt.Println("  OPEN  " + o)
			}
		} else {
			fmt.Println("  open children: none")
		}
	}
}

// ---------------------------------------------------------------------------
// issues / prs / milestone / comment / state

func cmdIssues(c *gitea.Client, owner, name string, args []string) {
	state := gitea.StateOpen
	opt := gitea.ListIssueOption{Type: gitea.IssueTypeIssue}
	var j bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "open":
			state = gitea.StateOpen
		case a == "closed":
			state = gitea.StateClosed
		case a == "all":
			state = gitea.StateAll
		case a == "-j":
			j = true
		case a == "-k" && i+1 < len(args):
			opt.KeyWord = args[i+1]
			i++
		case a == "-L" && i+1 < len(args):
			opt.Labels = []string{args[i+1]}
			i++
		case a == "-m" && i+1 < len(args):
			opt.Milestones = []string{args[i+1]}
			i++
		case a == "-A" && i+1 < len(args):
			opt.CreatedBy = args[i+1]
			i++
		}
	}
	opt.State = state
	var out []*gitea.Issue
	page := 1
	for {
		is, _, err := c.ListRepoIssues(owner, name, gitea.ListIssueOption{
			ListOptions: gitea.ListOptions{Page: page, PageSize: 50},
			State:       state, Type: opt.Type, Labels: opt.Labels,
			Milestones: opt.Milestones, KeyWord: opt.KeyWord, CreatedBy: opt.CreatedBy,
		})
		if err != nil {
			fatal("list issues: %v", err)
		}
		if len(is) == 0 {
			break
		}
		out = append(out, is...)
		if len(is) < 50 {
			break
		}
		page++
	}
	for _, i := range out {
		if j {
			emitJSON(map[string]any{"index": i.Index, "title": i.Title, "state": i.State, "labels": labelNames(i), "milestone": milestoneName(i)})
		} else {
			fmt.Printf("#%-4d [%s] %s\n", i.Index, i.State, i.Title)
		}
	}
}

func cmdIssueCreate(c *gitea.Client, owner, name string, args []string) {
	if len(args) < 1 {
		fatal(`usage: po issue-create "<title>" "<body>"`)
	}
	title := args[0]
	body := ""
	if len(args) > 1 {
		body = args[1]
	}
	if strings.HasPrefix(body, "@") {
		if raw, err := os.ReadFile(strings.TrimPrefix(body, "@")); err == nil {
			body = string(raw)
		}
	}
	iss, _, err := c.CreateIssue(owner, name, gitea.CreateIssueOption{Title: title, Body: body})
	if err != nil {
		fatal("create issue: %v", err)
	}
	fmt.Printf("created #%d: %s\n", iss.Index, iss.Title)
}

// cmdIssueShow prints an issue for PO review without python: body + state +
// labels + milestone + comments.
func cmdIssueShow(c *gitea.Client, owner, name string, args []string) {
	if len(args) < 1 {
		fatal("usage: po issue-show <n>")
	}
	idx := atoi(args[0])
	iss, _, err := c.GetIssue(owner, name, idx)
	if err != nil {
		fatal("issue #%d: %v", idx, err)
	}
	fmt.Printf("#%d [%s] %s\n", iss.Index, iss.State, iss.Title)
	if ms := milestoneName(iss); ms != "" {
		fmt.Printf("  milestone: %s\n", ms)
	}
	if ls := labelNames(iss); len(ls) > 0 {
		fmt.Printf("  labels: %s\n", strings.Join(ls, ", "))
	}
	fmt.Println("  --- body ---")
	if strings.TrimSpace(iss.Body) != "" {
		fmt.Println(iss.Body)
	} else {
		fmt.Println("(no body)")
	}
	fmt.Println("  --- comments ---")
	page := 1
	shown := 0
	for {
		comments, _, err := c.ListIssueComments(owner, name, idx, gitea.ListIssueCommentOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: 50},
		})
		if err != nil {
			fatal("comments #%d: %v", idx, err)
		}
		if len(comments) == 0 {
			break
		}
		for _, cm := range comments {
			author := "?"
			if cm.Poster != nil {
				author = cm.Poster.UserName
			}
			fmt.Printf("-- %s @ %s --\n%s\n", author, cm.Created.Format("2006-01-02 15:04"), cm.Body)
			shown++
		}
		if len(comments) < 50 {
			break
		}
		page++
	}
	if shown == 0 {
		fmt.Println("(no comments)")
	}
}

// cmdPRCreate creates a pull request (head branch, base branch, title, body)
// and prints the new PR number + URL. No curl/python.
func cmdPRCreate(c *gitea.Client, owner, name string, args []string) {
	if len(args) < 4 {
		fatal(`usage: po pr-create <head> <base> "<title>" "<body>"`)
	}
	head, base, title, body := args[0], args[1], args[2], args[3]
	if strings.HasPrefix(body, "@") {
		if raw, err := os.ReadFile(strings.TrimPrefix(body, "@")); err == nil {
			body = string(raw)
		}
	}
	pr, _, err := c.CreatePullRequest(owner, name, gitea.CreatePullRequestOption{
		Head: head, Base: base, Title: title, Body: body,
	})
	if err != nil {
		fatal("create pr: %v", err)
	}
	fmt.Printf("created PR #%d: %s\n", pr.Index, pr.Title)
	if pr.HTMLURL != "" {
		fmt.Println("  " + pr.HTMLURL)
	}
}

func labelNames(i *gitea.Issue) []string {
	var out []string
	for _, l := range i.Labels {
		out = append(out, l.Name)
	}
	return out
}

func milestoneName(i *gitea.Issue) string {
	if i.Milestone != nil {
		return i.Milestone.Title
	}
	return ""
}

func cmdComment(c *gitea.Client, owner, name string, args []string) {
	if len(args) < 2 {
		fatal("usage: po comment <index> \"<body>\"")
	}
	idx := atoi(args[0])
	body := args[1]
	if strings.HasPrefix(body, "@") {
		if raw, err := os.ReadFile(strings.TrimPrefix(body, "@")); err == nil {
			body = string(raw)
		}
	}
	if _, _, err := c.CreateIssueComment(owner, name, idx, gitea.CreateIssueCommentOption{Body: body}); err != nil {
		fatal("comment #%d: %v", idx, err)
	}
	fmt.Printf("comment added to #%d\n", idx)
}

func cmdState(c *gitea.Client, owner, name, state string, args []string) {
	if len(args) == 0 {
		fatal("usage: po %s <index> [index...]", state)
	}
	st := gitea.StateType(state)
	for _, a := range args {
		idx := atoi(a)
		opt := gitea.EditIssueOption{State: &st}
		if _, _, err := c.EditIssue(owner, name, idx, opt); err != nil {
			fatal("set #%d=%s: %v", idx, state, err)
		}
		fmt.Printf("#%d -> %s\n", idx, state)
	}
}

func cmdMilestones(c *gitea.Client, owner, name string, args []string) {
	state := gitea.StateAll
	if len(args) > 0 {
		switch args[0] {
		case "open":
			state = gitea.StateOpen
		case "closed":
			state = gitea.StateClosed
		case "all":
			state = gitea.StateAll
		}
	}
	page := 1
	for {
		ms, _, err := c.ListRepoMilestones(owner, name, gitea.ListMilestoneOption{
			ListOptions: gitea.ListOptions{Page: page, PageSize: 50},
			State:       state,
		})
		if err != nil {
			fatal("list milestones: %v", err)
		}
		if len(ms) == 0 {
			break
		}
		for _, m := range ms {
			fmt.Printf("[%s] %-20s %d open / %d closed\n", m.State, m.Title, m.OpenIssues, m.ClosedIssues)
		}
		if len(ms) < 50 {
			break
		}
		page++
	}
}

func mergeableLabel(p *gitea.PullRequest) string {
	if p.HasMerged {
		return "-"
	}
	if p.Mergeable {
		return "yes"
	}
	return "no"
}

func cmdPRs(c *gitea.Client, owner, name string, args []string) {
	state := gitea.StateOpen
	if len(args) > 0 {
		switch args[0] {
		case "closed":
			state = gitea.StateClosed
		case "all":
			state = gitea.StateAll
		}
	}
	page := 1
	for {
		prs, _, err := c.ListRepoPullRequests(owner, name, gitea.ListPullRequestsOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: 50},
			State:       state,
		})
		if err != nil {
			fatal("list prs: %v", err)
		}
		if len(prs) == 0 {
			break
		}
		for _, p := range prs {
			fmt.Printf("#%-4d [%-6s] m=%-3s %s\n", p.Index, p.State, mergeableLabel(p), p.Title)
		}
		if len(prs) < 50 {
			break
		}
		page++
	}
}

func cmdPR(c *gitea.Client, owner, name string, args []string) {
	if len(args) < 1 {
		fatal("usage: po pr <n>")
	}
	idx := atoi(args[0])
	pr, _, err := c.GetPullRequest(owner, name, idx)
	if err != nil {
		fatal("pr #%d: %v", idx, err)
	}
	head := pr.Head
	base := pr.Base
	headRef, headSha, baseRef := "?", "-", "?"
	if head != nil {
		headRef, headSha = head.Ref, shortSHA(head.Sha)
	}
	if base != nil {
		baseRef = base.Ref
	}
	fmt.Printf("#%d [%s] %s\n", pr.Index, pr.State, pr.Title)
	fmt.Printf("  mergeable: %-3s  merged: %v\n", mergeableLabel(pr), pr.HasMerged)
	fmt.Printf("  head: %s  %s\n", headRef, headSha)
	fmt.Printf("  base: %s\n", baseRef)
	files, _, err := c.ListPullRequestFiles(owner, name, idx, gitea.ListPullRequestFilesOptions{})
	if err != nil {
		fatal("pr files #%d: %v", idx, err)
	}
	if len(files) == 0 {
		fmt.Println("  files: (none)")
		return
	}
	fmt.Println("  files:")
	for _, f := range files {
		fmt.Printf("    %-6s %s  +%d -%d\n", f.Status, f.Filename, f.Additions, f.Deletions)
	}
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// ---------------------------------------------------------------------------
// CI runs

func runStatus(r *gitea.ActionWorkflowRun) string {
	if r.Conclusion != "" {
		return r.Conclusion
	}
	if r.Status == "" {
		return "unknown"
	}
	return r.Status // queued / in_progress / waiting / completed
}

func cmdRun(c *gitea.Client, owner, name string, args []string) {
	if len(args) < 1 {
		fatal("usage: po run <branch>")
	}
	branch := args[0]
	resp, _, err := c.ListRepoActionRuns(owner, name, gitea.ListRepoActionRunsOptions{
		ListOptions: gitea.ListOptions{Page: 1, PageSize: 10},
		Branch:      branch,
	})
	if err != nil {
		fatal("list runs %s: %v", branch, err)
	}
	runs := resp.WorkflowRuns
	if len(runs) == 0 {
		fmt.Printf("run %s: no workflow runs\n", branch)
		return
	}
	fmt.Printf("run %s — %d run(s)\n", branch, len(runs))
	for _, r := range runs {
		fmt.Printf("  #%-4d %-8d %-12s %-8s %s\n", r.RunNumber, r.ID, r.Status, orDash(runStatus(r)), r.DisplayTitle)
	}
	head := runs[0]
	fmt.Printf("HEAD run %d: status=%s conclusion=%s\n  %s\n", head.ID, head.Status, orDash(head.Conclusion), head.HTMLURL)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func cmdMerge(c *gitea.Client, owner, name string, args []string) {
	if len(args) < 1 {
		fatal("usage: po merge <n>")
	}
	idx := atoi(args[0])
	del := true
	ok, _, err := c.MergePullRequest(owner, name, idx, gitea.MergePullRequestOption{
		Style:                  gitea.MergeStyleMerge,
		DeleteBranchAfterMerge: &del,
	})
	if err != nil {
		fatal("merge #%d: %v", idx, err)
	}
	if !ok {
		fmt.Printf("merge #%d: rejected (not merged — check state/mergeable)\n", idx)
		return
	}
	// Definitive confirmation without grep: re-fetch the PR and print merged
	// state + URL.
	pr, _, err := c.GetPullRequest(owner, name, idx)
	if err != nil {
		fatal("confirm #%d: %v", idx, err)
	}
	line := fmt.Sprintf("merged #%d (merged: %v, branch deleted)", idx, pr.HasMerged)
	if pr.HTMLURL != "" {
		line += "\n  " + pr.HTMLURL
	}
	fmt.Println(line)
}

var reANSIEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func cmdJobLogs(c *gitea.Client, owner, name string, args []string) {
	if len(args) < 1 {
		fatal("usage: po joblogs <run>")
	}
	runID := atoi(args[0])
	jobs, _, err := c.ListRepoActionRunJobs(owner, name, runID, gitea.ListRepoActionJobsOptions{})
	if err != nil {
		fatal("jobs for run %d: %v", runID, err)
	}
	var failed []*gitea.ActionWorkflowJob
	for _, j := range jobs.Jobs {
		if j.Conclusion == "failure" {
			failed = append(failed, j)
		}
	}
	if len(failed) == 0 {
		fmt.Printf("run %d: no failed jobs\n", runID)
		return
	}
	for _, j := range failed {
		fmt.Printf("job %d %s: FAILED\n", j.ID, j.Name)
		for _, s := range j.Steps {
			mark := "  "
			if s.Conclusion == "failure" {
				mark = "> "
			}
			fmt.Printf("%sstep %d %s: %s\n", mark, s.Number, s.Name, orDash(s.Conclusion))
		}
		log, _, err := c.GetRepoActionJobLogs(owner, name, j.ID)
		if err != nil {
			fatal("logs for job %d: %v", j.ID, err)
		}
		printTail(log, 40)
	}
}

func printTail(log []byte, n int) {
	lines := bytes.Split(log, []byte{'\n'})
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	for _, l := range lines[start:] {
		if len(l) == 0 {
			continue
		}
		fmt.Println(reANSIEscape.ReplaceAllString(string(l), ""))
	}
}

// ---------------------------------------------------------------------------
// diff scan (secrets + hardcoded paths) — report file:line only, never values

type scanHit struct {
	file string
	line int
	cat  string
	desc string // masked detail; never the secret value
}

type secretPat struct {
	name string
	re   *regexp.Regexp
}

// maskGroup returns "key=***" when the pattern captures the key name, else "".
func (p secretPat) mask(content string, loc []int) string {
	m := p.re.FindStringSubmatchIndex(content)
	if m == nil || len(m) < 4 || m[2] < 0 {
		return ""
	}
	return content[m[2]:m[3]] + "=***"
}

var secretPatterns = []secretPat{
	{"aws-access-key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"github-token", regexp.MustCompile(`\bgh[pousr]_[0-9A-Za-z]{20,}\b`)},
	{"slack-token", regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}\b`)},
	{"stripe-key", regexp.MustCompile(`\bsk_live_[0-9A-Za-z]{16,}\b`)},
	{"aws-secret", regexp.MustCompile(`(?i)\b(aws_secret_access_key)\b\s*[:=]`)},
	// Generic key=value: only keys whose name signals a secret (token/secret/
	// password/…), value length >= 10. Keeps `restart:`, `type:`, `err := …`
	// out of the report so real hits are not drowned in noise.
	{"generic-key", regexp.MustCompile(`(?i)\b([a-z0-9_]*(?:secret|token|password|passwd|api[_-]?key|credential|auth)[a-z0-9_]*)\b\s*[:=]\s*["']?[0-9A-Za-z_\-/+.]{10,}`)},
}

var (
	reNewFile = regexp.MustCompile(`^\+\+\+ b/(.*)$`)
	reHunk    = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	reAbsPath = regexp.MustCompile(`/(?:mnt|home|Users)/`)
)

func scanDiff(data []byte) []scanHit {
	var hits []scanHit
	file := ""
	newLine := 0
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			file = strings.TrimPrefix(line, "+++ b/")
			if i := strings.IndexByte(file, '\t'); i >= 0 {
				file = file[:i]
			}
			newLine = 0
		case strings.HasPrefix(line, "@@ "):
			if m := reHunk.FindStringSubmatch(line); m != nil {
				newLine, _ = strconv.Atoi(m[2])
			}
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			content := line[1:]
			for _, p := range secretPatterns {
				if loc := p.re.FindStringIndex(content); loc != nil {
					hits = append(hits, scanHit{file, newLine, "secret:" + p.name, p.mask(content, loc)})
					break
				}
			}
			if reAbsPath.MatchString(content) {
				// file:line + category only — never print the offending path.
				hits = append(hits, scanHit{file, newLine, "hardcoded-path", ""})
			}
			newLine++
		case strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "++"):
			newLine++
		}
	}
	return hits
}

func cmdScan(c *gitea.Client, owner, name string, args []string) {
	if len(args) < 1 {
		fatal("usage: po scan <branch> [base]")
	}
	head := args[0]
	base := envOr("PO_BASE", defBase)
	if len(args) > 1 {
		base = args[1]
	}
	cmp, _, err := c.CompareCommits(owner, name, base, head)
	if err != nil {
		fatal("compare %s...%s: %v", base, head, err)
	}
	var diffs []byte
	for _, commit := range cmp.Commits {
		d, _, err := c.GetCommitDiff(owner, name, commit.SHA)
		if err != nil {
			fatal("diff %s: %v", shortSHA(commit.SHA), err)
		}
		diffs = append(diffs, d...)
	}
	hits := scanDiff(diffs)
	if len(hits) == 0 {
		fmt.Printf("scan %s vs %s: clean (%d commits)\n", head, base, len(cmp.Commits))
		return
	}
	fmt.Printf("scan %s vs %s: %d hit(s) in %d commit(s)\n", head, base, len(hits), len(cmp.Commits))
	for _, h := range hits {
		desc := h.desc
		if desc == "" {
			desc = h.cat
		}
		fmt.Printf("  %s:%d  %-22s %s\n", h.file, h.line, h.cat, desc)
	}
}

func atoi(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fatal("bad index %q", s)
	}
	return n
}

func emitJSON(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}
