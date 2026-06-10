package fabric

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	sdk "code.gitea.io/sdk/gitea"
)

// Webhook payload structs. The Gitea SDK does not export webhook payload types
// (they live in the Gitea server module), so the few fields used here are
// declared locally.
type giteaWebhookPayload struct {
	Action      string              `json:"action"`
	Number      int64               `json:"number"`
	Issue       *webhookIssue       `json:"issue"`
	PullRequest *webhookPullRequest `json:"pull_request"`
	Repository  *webhookRepository  `json:"repository"`
	Sender      *webhookUser        `json:"sender"`
	// Commit status fields ("status" event, set by Gitea Actions).
	SHA     string `json:"sha"`
	State   string `json:"state"`
	Context string `json:"context"`
}

type webhookIssue struct {
	Number int64  `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

type webhookPullRequest struct {
	Number int64         `json:"number"`
	Title  string        `json:"title"`
	Body   string        `json:"body"`
	State  string        `json:"state"`
	Merged bool          `json:"merged"`
	Head   webhookBranch `json:"head"`
	Base   webhookBranch `json:"base"`
}

type webhookBranch struct {
	Ref string `json:"ref"`
	Sha string `json:"sha"`
}

type webhookRepository struct {
	FullName string `json:"full_name"`
}

type webhookUser struct {
	Login string `json:"login"`
}

func (p *giteaWebhookPayload) ownerRepo() (string, string, bool) {
	if p.Repository == nil {
		return "", "", false
	}
	parts := strings.Split(p.Repository.FullName, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// configuredRepo extracts owner/repo from the payload and rejects events for
// any repository other than the one this handler is configured for. This
// guards against org-wide webhooks fanning agent work out to foreign repos.
func (h *IssueHandler) configuredRepo(p giteaWebhookPayload) (string, string, bool) {
	owner, repo, ok := p.ownerRepo()
	if !ok || owner != h.Cfg.Gitea.Owner || repo != h.Cfg.Gitea.Repo {
		return "", "", false
	}
	return owner, repo, true
}

// validSignature checks the Gitea HMAC-SHA256 webhook signature.
// An empty configured secret disables validation.
func (h *IssueHandler) validSignature(body []byte, signature string) bool {
	secret := h.Cfg.Issue.Webhook.Secret
	if secret == "" {
		return true
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (h *IssueHandler) ServeWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "Error reading body", http.StatusRequestEntityTooLarge)
		return
	}

	if !h.validSignature(body, r.Header.Get("X-Gitea-Signature")) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var payload giteaWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Error parsing JSON", http.StatusBadRequest)
		return
	}

	event := r.Header.Get("X-Gitea-Event")
	fmt.Printf("[webhook] Received event: %s, action: %s\n", event, payload.Action)

	switch event {
	case "issues":
		h.handleIssueWebhook(payload)
	case "pull_request":
		h.handlePullRequestWebhook(payload)
	case "status":
		h.handleCommitStatusWebhook(payload)
	default:
		fmt.Printf("[webhook] Unhandled event: %s\n", event)
	}

	w.WriteHeader(http.StatusOK)
}

// handleIssueWebhook reacts to new issues immediately: the architect analyses
// the task without waiting for the next poll cycle.
func (h *IssueHandler) handleIssueWebhook(payload giteaWebhookPayload) {
	if payload.Issue == nil || payload.Action != "opened" {
		return
	}
	// Issues created by the fabric's own users must not re-trigger the fabric.
	if payload.Sender != nil && h.isBotLogin(payload.Sender.Login) {
		fmt.Printf("[webhook] Ignoring issue from own user %s\n", payload.Sender.Login)
		return
	}
	owner, repo, ok := h.configuredRepo(payload)
	if !ok {
		return
	}
	num := payload.Issue.Number
	fmt.Printf("[webhook] Issue #%d opened. Triggering architect analysis...\n", num)

	go func() {
		if !h.tryClaim(h.busyIssues, num) {
			return
		}
		defer h.release(h.busyIssues, num)

		issue, err := h.Architect.GetIssue(owner, repo, num)
		if err != nil {
			fmt.Printf("[webhook] Failed to load issue #%d: %v\n", num, err)
			return
		}
		if err := h.RunArchitectStage(issue); err != nil {
			fmt.Printf("[webhook] Architect stage for issue #%d failed: %v\n", num, err)
		}
	}()
}

func (h *IssueHandler) handlePullRequestWebhook(payload giteaWebhookPayload) {
	if payload.PullRequest == nil {
		return
	}
	owner, repo, ok := h.configuredRepo(payload)
	if !ok {
		return
	}
	pr := payload.PullRequest
	fmt.Printf("[webhook] PR #%d %s\n", pr.Number, payload.Action)

	switch payload.Action {
	case "opened", "synchronized":
		go h.ReviewPullRequest(owner, repo, pr.Number)
	case "closed":
		if pr.Merged {
			h.markLinkedIssueCompleted(owner, repo, pr.Body)
		}
	}
}

// markLinkedIssueCompleted closes the loop after a merge: the issue linked via
// "Closes #N" gets the terminal completed status, its architect task checklist
// is ticked, and the issue is closed unless another open PR still references it.
func (h *IssueHandler) markLinkedIssueCompleted(owner, repo, prBody string) {
	match := closesIssueRegex.FindStringSubmatch(prBody)
	if len(match) < 2 {
		return
	}
	num, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return
	}
	fmt.Printf("[webhook] PR merged. Marking issue #%d as completed\n", num)
	_ = h.SetStatusLabel(num, "completed")
	_ = upsertStatus(h.Developer, owner, repo, num, func(s *workStatus) {
		s.Stage = stageCompleted
	})

	if h.issueReferencedByOpenPR(owner, repo, num) {
		fmt.Printf("[webhook] Issue #%d still referenced by another open PR, leaving open\n", num)
		return
	}
	h.completeIssueTasks(owner, repo, num)
	if err := h.Developer.CloseIssue(owner, repo, num); err != nil {
		fmt.Printf("[webhook] Failed to close issue #%d: %v\n", num, err)
	}
}

// issueReferencedByOpenPR reports whether any open PR still closes this issue.
func (h *IssueHandler) issueReferencedByOpenPR(owner, repo string, num int64) bool {
	prs, err := h.Developer.ListOpenPullRequests(owner, repo)
	if err != nil {
		return false
	}
	ref := fmt.Sprintf("Closes #%d", num)
	return slices.ContainsFunc(prs, func(pr *sdk.PullRequest) bool {
		return strings.Contains(pr.Body, ref)
	})
}

// completeIssueTasks ticks the architect's task checklist in the issue body
// once the implementing PR has merged with green checks.
func (h *IssueHandler) completeIssueTasks(owner, repo string, num int64) {
	issue, err := h.Developer.GetIssue(owner, repo, num)
	if err != nil {
		return
	}
	start := strings.Index(issue.Body, ArchStart)
	end := strings.Index(issue.Body, ArchEnd)
	if start < 0 || end < 0 || end < start {
		return
	}
	block := issue.Body[start:end]
	ticked := strings.ReplaceAll(block, "- [ ]", "- [x]")
	if ticked == block {
		return
	}
	newBody := issue.Body[:start] + ticked + issue.Body[end:]
	_ = h.Developer.UpdateIssueBody(owner, repo, num, newBody)
}

// handleCommitStatusWebhook reacts to CI failures reported as commit statuses
// (Gitea Actions sets these). The failure is correlated to the single open PR
// whose head SHA matches (no fan-out).
func (h *IssueHandler) handleCommitStatusWebhook(payload giteaWebhookPayload) {
	fmt.Printf("[webhook] Commit status %s on %s (%s)\n", payload.State, payload.SHA, payload.Context)
	if payload.State != "failure" && payload.State != "error" {
		return
	}
	owner, repo, ok := h.configuredRepo(payload)
	if !ok {
		return
	}

	prs, err := h.Developer.ListOpenPullRequests(owner, repo)
	if err != nil {
		fmt.Printf("[webhook] Failed to list PRs: %v\n", err)
		return
	}
	for _, pr := range prs {
		if pr.Head == nil || pr.Head.Sha != payload.SHA {
			continue
		}
		fmt.Printf("[webhook] CI failure on PR #%d (sha %s). Launching fixer...\n", pr.Index, payload.SHA)
		go h.FixPullRequest(owner, repo, pr.Index, payload.SHA)
		return
	}
	fmt.Printf("[webhook] No open PR found for failed sha %s, checking base branch\n", payload.SHA)
	go h.fileMainCIFailureIssue(owner, repo, payload.SHA, payload.Context)
}

// ciFailureIssueTitle is the dedup key for one automated issue per failed base commit.
func ciFailureIssueTitle(baseBranch, sha string) string {
	short := sha
	if len(short) > 7 {
		short = short[:7]
	}
	return fmt.Sprintf("[ci] %s check failure at %s", baseBranch, short)
}

// commitOnBaseBranch reports whether sha is reachable from the configured base branch.
func (h *IssueHandler) commitOnBaseBranch(sha string) bool {
	if sha == "" || h.Cfg.RootDir == "" {
		return false
	}
	base := h.Cfg.Issue.BaseBranch
	if base == "" {
		base = "main"
	}
	ref := "origin/" + base
	_, _ = h.runCommand(h.Cfg.RootDir, nil, "git", "fetch", "origin", base)
	_, err := h.runCommand(h.Cfg.RootDir, nil, "git", "merge-base", "--is-ancestor", sha, ref)
	return err == nil
}

// fileMainCIFailureIssue opens a tracked issue when CI fails on the base branch after
// merge (no open PR owns the failing head SHA). The regular architect-developer flow
// picks it up on the next poll cycle.
func (h *IssueHandler) fileMainCIFailureIssue(owner, repo, sha, checkContext string) {
	base := h.Cfg.Issue.BaseBranch
	if base == "" {
		base = "main"
	}
	if !h.commitOnBaseBranch(sha) {
		fmt.Printf("[webhook] Failed sha %s is not on %s, ignoring\n", sha, base)
		return
	}

	title := ciFailureIssueTitle(base, sha)
	if issues, err := h.Developer.ListOpenIssues(owner, repo); err == nil {
		for _, issue := range issues {
			if issue.Title == title {
				fmt.Printf("[webhook] CI failure issue already filed: #%d\n", issue.Index)
				return
			}
		}
	}

	commitMsg, _ := h.runCommand(h.Cfg.RootDir, nil, "git", "log", "-1", "--format=%s", sha)
	baseURL := strings.TrimRight(h.Cfg.Gitea.BaseURL, "/")
	body := fmt.Sprintf(`CI failed on %s after a merge or direct push. No open PR matches this commit, so this issue was opened automatically.

## Failure
- Branch: %s
- Commit: %s
- Check: %s
- Message: %s
- Actions: %s/%s/%s/actions

## Task
Reproduce on %s, fix the failing checks
(./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh), open a PR, and verify CI is green on %s.
`, base, base, sha, checkContext, commitMsg, baseURL, owner, repo, base, base)

	issue, err := h.Developer.CreateIssue(owner, repo, title, body)
	if err != nil {
		fmt.Printf("[webhook] Failed to file CI failure issue for %s: %v\n", sha, err)
		return
	}
	fmt.Printf("[webhook] Filed CI failure issue #%d for %s on %s\n", issue.Index, sha, base)
}

// SyncPullRequest merges the base branch into a PR head and fixes check failures.
// It runs for conflicted PRs and for approved PRs that fell behind base.
func (h *IssueHandler) SyncPullRequest(owner, repo string, prNum int64) {
	if !h.tryClaim(h.busyPRs, prNum) {
		return
	}
	defer h.release(h.busyPRs, prNum)
	h.doSyncPullRequest(owner, repo, prNum)
}

// prBehindBase reports whether ref has commits not present in headSHA.
func (h *IssueHandler) prBehindBase(baseRef, headSHA string) bool {
	if headSHA == "" || h.Cfg.RootDir == "" {
		return false
	}
	base := strings.TrimPrefix(baseRef, "origin/")
	remote := "origin/" + base
	_, _ = h.runCommand(h.Cfg.RootDir, nil, "git", "fetch", "origin", base)
	_, err := h.runCommand(h.Cfg.RootDir, nil, "git", "merge-base", "--is-ancestor", remote, headSHA)
	return err != nil
}

func (h *IssueHandler) doSyncPullRequest(owner, repo string, prNum int64) {
	pr, err := h.Developer.GetPullRequest(owner, repo, prNum)
	if err != nil || pr.State != "open" || pr.Head == nil {
		return
	}
	if !h.shouldSyncPullRequest(owner, repo, pr) {
		return
	}
	reason := "conflicts with"
	if pr.Mergeable {
		reason = "approved and behind"
	}
	fmt.Printf("[sync] PR #%d %s %s. Merging base and resolving...\n", prNum, reason, pr.Base.Ref)

	branch := pr.Head.Ref
	localBranch := fmt.Sprintf("sync/pr-%d", prNum)
	path := h.worktreePath(fmt.Sprintf("pr-%d", prNum))
	if err := h.ensureWorktree(localBranch, path, "origin/"+branch); err != nil {
		fmt.Printf("[sync] Failed to prepare worktree for PR #%d: %v\n", prNum, err)
		return
	}

	mergeOut, mergeErr := h.runCommand(path, h.gitIdentityEnv(), "git", "merge", "--no-edit", "origin/"+pr.Base.Ref)
	if mergeErr != nil {
		conflicts, _ := h.runCommand(path, nil, "git", "diff", "--name-only", "--diff-filter=U")
		fmt.Printf("[sync] Merge conflicts on PR #%d:\n%s\n", prNum, conflicts)

		promptPath := filepath.Join(path, ".issue-agent-prompt.md")
		prompt := fmt.Sprintf(`# Resolve merge conflicts for PR #%d

Title: %s

## Task
A merge of origin/%s into this branch is in progress and stopped on conflicts.
Resolve every conflict, preserving both the intent of this PR and the changes
from the base branch. Then stage the files and complete the merge commit
(git add <files> && git commit --no-edit). Finally run
./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh and fix any failures.

## Conflicted files
%s

## Merge output
%s

## Constraints (inviolable, see docs/skills/engineering-principles.md)
- Resolve conflicts only; no unrelated changes
- Never discard base branch changes to make conflicts disappear
`, prNum, pr.Title, pr.Base.Ref, conflicts, mergeOut)
		if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
			return
		}
		if out, err := h.runAgent(path, promptPath, h.Developer); err != nil {
			fmt.Printf("[sync] Conflict agent failed for PR #%d: %v: %s\n", prNum, err, out)
			_, _ = h.runCommand(path, nil, "git", "merge", "--abort")
			return
		}
		if unresolved, _ := h.runCommand(path, nil, "git", "diff", "--name-only", "--diff-filter=U"); unresolved != "" {
			fmt.Printf("[sync] PR #%d still has unresolved conflicts, aborting:\n%s\n", prNum, unresolved)
			_, _ = h.runCommand(path, nil, "git", "merge", "--abort")
			return
		}
	} else if strings.Contains(mergeOut, "Already up to date") {
		fmt.Printf("[sync] PR #%d already up to date with %s\n", prNum, pr.Base.Ref)
		return
	}

	if err := h.commitFix(path, localBranch, branch, "chore: merge base branch and resolve conflicts"); err != nil {
		fmt.Printf("[sync] Failed to push merge for PR #%d: %v\n", prNum, err)
		return
	}
	fmt.Printf("[sync] Pushed base merge for PR #%d\n", prNum)

	pr, err = h.Developer.GetPullRequest(owner, repo, prNum)
	if err != nil || pr.Head == nil {
		return
	}
	if checkOut, checkErr := h.runPullRequestChecks(path, owner, repo, prNum); checkErr != nil {
		fmt.Printf("[sync] Checks failing after base merge on PR #%d, launching fix\n", prNum)
		h.fixPullRequestCore(owner, repo, pr, path, localBranch, branch, pr.Head.Sha, checkOut)
	}
}

// FixPullRequest lets the developer agent fix a CI failure on a PR, within a
// bounded budget; when the budget is exhausted the architect reviews the design
// once and the PR is handed over to a human.
func (h *IssueHandler) FixPullRequest(owner, repo string, prNum int64, headSHA string) {
	if !h.claimWithRetry(h.busyPRs, prNum, 6, 20*time.Second) {
		fmt.Printf("[fixer] PR #%d stayed busy, skipping fix\n", prNum)
		return
	}
	defer h.release(h.busyPRs, prNum)

	pr, err := h.Developer.GetPullRequest(owner, repo, prNum)
	if err != nil {
		fmt.Printf("[fixer] Failed to load PR #%d: %v\n", prNum, err)
		return
	}
	if pr.State != "open" || pr.Head == nil {
		return
	}
	if headSHA == "" {
		headSHA = pr.Head.Sha
	}
	if headSHA != pr.Head.Sha {
		fmt.Printf("[fixer] PR #%d head moved on from %s, skipping stale failure\n", prNum, headSHA)
		return
	}

	branch := pr.Head.Ref
	localBranch := fmt.Sprintf("fix/pr-%d", prNum)
	path := h.worktreePath(fmt.Sprintf("pr-%d", prNum))
	if err := h.ensureWorktree(localBranch, path, "origin/"+branch); err != nil {
		fmt.Printf("[fixer] Failed to prepare worktree for PR #%d: %v\n", prNum, err)
		return
	}

	checkOut, checkErr := h.runPullRequestChecks(path, owner, repo, prNum)
	if checkErr == nil {
		fmt.Printf("[fixer] Checks pass locally for PR #%d, nothing to fix\n", prNum)
		return
	}
	h.fixPullRequestCore(owner, repo, pr, path, localBranch, branch, headSHA, checkOut)
}

func (h *IssueHandler) fixPullRequestCore(owner, repo string, pr *sdk.PullRequest, path, localBranch, branch, headSHA, checkOut string) {
	prNum := pr.Index
	state := loadStatus(h.Developer, owner, repo, prNum)
	cfg := h.Cfg.Issue.Webhook
	if state.Escalated {
		return
	}
	if state.CIFix[headSHA] >= cfg.CIFixMaxPerSHA || state.totalCIFixes() >= cfg.CIFixMaxPerPR {
		h.escalateToArchitect(owner, repo, pr)
		return
	}

	promptPath := filepath.Join(path, ".issue-agent-prompt.md")
	prompt := fmt.Sprintf(`# CI failure fix for PR #%d

Title: %s

## Task
CI failed on branch %s. Reproduce and fix the failures, then make
./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh pass. Commit the fix.

## Failing check output
%s

## Constraints (inviolable, see docs/skills/engineering-principles.md)
- Minimal fix scoped to the failure; no work for work's sake
- Reuse existing packages and libraries before writing new code
- No mock-based or isolated unit tests
`, prNum, pr.Title, branch, checkOut)
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		fmt.Printf("[fixer] Failed to write fix prompt for PR #%d: %v\n", prNum, err)
		return
	}

	out, err := h.runAgent(path, promptPath, h.Developer)
	if err != nil {
		fmt.Printf("[fixer] Fix agent failed for PR #%d: %v: %s\n", prNum, err, out)
		return
	}

	if out, err := h.runChecks(path); err != nil {
		fmt.Printf("[fixer] Checks still failing for PR #%d after fix: %s\n", prNum, out)
		return
	}
	if out, err := h.runPullRequestChecks(path, owner, repo, prNum); err != nil {
		fmt.Printf("[fixer] PR policy still failing for PR #%d after fix: %s\n", prNum, out)
		return
	}

	if err := h.commitFix(path, localBranch, branch, "fix(ci): repair failing checks"); err != nil {
		fmt.Printf("[fixer] Failed to push fix for PR #%d: %v\n", prNum, err)
		return
	}

	_ = upsertStatus(h.Developer, owner, repo, prNum, func(s *workStatus) {
		if s.CIFix == nil {
			s.CIFix = map[string]int{}
		}
		s.CIFix[headSHA]++
		s.Stage = stageDeveloper
	})
	fmt.Printf("[fixer] Pushed fix for PR #%d\n", prNum)
}

func (h *IssueHandler) commitFix(path, localBranch, remoteBranch, message string) error {
	status, err := h.runCommand(path, nil, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		if _, err := h.runCommand(path, nil, "git", "add", "."); err != nil {
			return err
		}
		if out, err := h.runCommand(path, h.gitIdentityEnv(), "git", "commit", "-m", message); err != nil {
			return fmt.Errorf("commit failed: %s: %w", out, err)
		}
	}
	return h.gitPush(path, localBranch, remoteBranch)
}

// escalateToArchitect posts a one-time design review on a PR whose CI fix
// budget is exhausted and hands the work over to a human.
func (h *IssueHandler) escalateToArchitect(owner, repo string, pr *sdk.PullRequest) {
	prNum := pr.Index
	fmt.Printf("[fixer] Fix budget exhausted for PR #%d, escalating to architect\n", prNum)

	branch := pr.Head.Ref
	path := h.worktreePath(fmt.Sprintf("pr-%d", prNum))
	analysis := "Automated fixes did not converge; the solution design likely needs revision."
	if err := h.ensureWorktree(fmt.Sprintf("review/pr-%d", prNum), path, "origin/"+branch); err == nil {
		checkOut, _ := h.runChecks(path)
		promptPath := filepath.Join(path, ".issue-architect-prompt.md")
		prompt := fmt.Sprintf(`# Design review for failing PR #%d

Title: %s

You are acting as Solution Architect. The developer agent exhausted its CI fix
budget on this PR. Analyse whether the failures stem from a design problem.
Produce a concise markdown review:

## Diagnosis
- Root cause of the repeated CI failures

## Design Verdict
- Is the chosen approach sound? If not, what structure should replace it?

## Recommended Next Steps
- Concrete, minimal actions for a human or a fresh implementation attempt

## Failing check output
%s
`, prNum, pr.Title, checkOut)
		if err := os.WriteFile(promptPath, []byte(prompt), 0644); err == nil {
			if out, err := h.runAgent(path, promptPath, h.Architect); err == nil && strings.TrimSpace(out) != "" {
				analysis = out
			}
		}
	}

	_, _ = h.Architect.CreateIssueComment(owner, repo, prNum,
		"## Architect escalation\n\n"+analysis+"\n\nAutomated remediation is stopped for this PR. Human attention required.")
	_ = h.Developer.AddIssueLabel(owner, repo, prNum, "status:needs_human")

	_ = upsertStatus(h.Developer, owner, repo, prNum, func(s *workStatus) {
		s.Stage = stageEscalated
		s.Escalated = true
	})

	// Propagate to the linked issue so the poller stops retrying it.
	if match := closesIssueRegex.FindStringSubmatch(pr.Body); len(match) == 2 {
		if issueNum, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			_ = h.SetStatusLabel(issueNum, "needs_human")
		}
	}
}

// ReviewPullRequest posts a single automated review per PR head SHA. On
// REQUEST_CHANGES the developer addresses the in-scope findings and pushes,
// which triggers a fresh review of the new head; out-of-scope findings are
// spun off into a separate issue so the developer is not dragged beyond the
// task's boundaries.
func (h *IssueHandler) ReviewPullRequest(owner, repo string, prNum int64) {
	// The PR may still be claimed by the review-fix push that triggered this
	// review; retry briefly instead of dropping the event.
	if !h.claimWithRetry(h.busyPRs, prNum, 4, 20*time.Second) {
		fmt.Printf("[reviewer] PR #%d stayed busy, skipping review\n", prNum)
		return
	}
	defer h.release(h.busyPRs, prNum)

	pr, err := h.Reviewer.GetPullRequest(owner, repo, prNum)
	if err != nil {
		fmt.Printf("[reviewer] Failed to load PR #%d: %v\n", prNum, err)
		return
	}
	if pr.State != "open" || pr.Head == nil {
		return
	}
	headSHA := pr.Head.Sha

	state := loadStatus(h.Developer, owner, repo, prNum)
	if state.hasReviewed(headSHA) {
		fmt.Printf("[reviewer] PR #%d head %s already reviewed, skipping\n", prNum, headSHA)
		return
	}

	branch := pr.Head.Ref
	path := h.worktreePath(fmt.Sprintf("pr-%d-review", prNum))
	if err := h.ensureWorktree(fmt.Sprintf("review/pr-%d-head", prNum), path, "origin/"+branch); err != nil {
		fmt.Printf("[reviewer] Failed to prepare worktree for PR #%d: %v\n", prNum, err)
		return
	}

	diff, _ := h.runCommand(path, nil, "git", "diff", "origin/"+pr.Base.Ref+"...HEAD")
	if len(diff) > 60000 {
		diff = diff[:60000] + "\n\n(diff truncated)"
	}

	promptPath := filepath.Join(path, ".issue-agent-prompt.md")
	prompt := fmt.Sprintf(`# Review PR #%d

Title: %s

You are acting as code reviewer. Review the diff below against
docs/skills/engineering-principles.md: reject duplication, speculative
abstractions, mock-based tests, and violations of the 3-tier boundaries.
Verify the change traces to its issue and reuses existing code where possible.

Scope rule: only findings that block this PR's linked issue belong in scope.
Real problems visible in the diff but beyond the issue's goal are out of
scope — they will be filed as a separate issue, do not demand them here.
REQUEST_CHANGES is justified by in-scope findings only.

Output EXACTLY this markdown structure and nothing else:

Verdict: APPROVE or REQUEST_CHANGES

## In-Scope Findings
- bullet list with file references, or "- none"

## Out-of-Scope Findings
- bullet list with file references, or "- none"

## PR body
%s

## Diff
%s
`, prNum, pr.Title, pr.Body, diff)
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return
	}

	out, err := h.runAgent(path, promptPath, h.Reviewer)
	if err != nil {
		fmt.Printf("[reviewer] Review agent failed for PR #%d: %v\n", prNum, err)
		return
	}

	_, _ = h.Reviewer.CreateIssueComment(owner, repo, prNum, "## Automated review\n\n"+out)
	_ = upsertStatus(h.Developer, owner, repo, prNum, func(s *workStatus) {
		s.ReviewedSHAs = append(s.ReviewedSHAs, headSHA)
	})

	requestChanges, inScope, outScope := parseReview(out)
	_ = upsertStatus(h.Developer, owner, repo, prNum, func(s *workStatus) {
		if requestChanges {
			s.ApprovedForMerge = false
		} else if strings.Contains(out, "Verdict: APPROVE") {
			s.ApprovedForMerge = true
		}
	})
	if outScope != "" {
		h.fileFollowUpIssue(owner, repo, pr, outScope)
	}
	if requestChanges && inScope != "" {
		h.fixReviewFindings(owner, repo, pr, inScope, state.ReviewFixes)
		return
	}
	if !requestChanges && strings.Contains(out, "Verdict: APPROVE") {
		h.doSyncPullRequest(owner, repo, prNum)
	}
}

// parseReview extracts the verdict and the two findings sections from the
// reviewer's structured output. "none" sections collapse to empty strings.
func parseReview(out string) (requestChanges bool, inScope, outScope string) {
	requestChanges = strings.Contains(out, "REQUEST_CHANGES")
	inScope = reviewSection(out, "## In-Scope Findings")
	outScope = reviewSection(out, "## Out-of-Scope Findings")
	return requestChanges, inScope, outScope
}

func reviewSection(out, header string) string {
	_, after, ok := strings.Cut(out, header)
	if !ok {
		return ""
	}
	if idx := strings.Index(after, "\n## "); idx >= 0 {
		after = after[:idx]
	}
	section := strings.TrimSpace(after)
	if section == "" || strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(section, "-")), "none") {
		return ""
	}
	return section
}

// fileFollowUpIssue records out-of-scope review findings as a new issue so the
// regular architect-developer flow handles them in their own bounded context.
func (h *IssueHandler) fileFollowUpIssue(owner, repo string, pr *sdk.PullRequest, findings string) {
	title := fmt.Sprintf("[follow-up] Out-of-scope review findings from PR #%d", pr.Index)

	// One follow-up issue per PR: further review rounds extend nothing.
	if issues, err := h.Reviewer.ListOpenIssues(owner, repo); err == nil {
		for _, issue := range issues {
			if issue.Title == title {
				return
			}
		}
	}

	body := fmt.Sprintf(`The automated review of PR #%d (%s) surfaced real problems that are
beyond that PR's scope. They are collected here to be handled separately.

## Findings
%s
`, pr.Index, pr.Title, findings)
	issue, err := h.Reviewer.CreateIssue(owner, repo, title, body)
	if err != nil {
		fmt.Printf("[reviewer] Failed to file follow-up issue for PR #%d: %v\n", pr.Index, err)
		return
	}
	fmt.Printf("[reviewer] Filed follow-up issue #%d for out-of-scope findings of PR #%d\n", issue.Index, pr.Index)
}

// fixReviewFindings lets the developer address in-scope review findings on the
// PR branch, within a bounded number of rounds. The caller must hold the busy
// claim for the PR. A successful push triggers a fresh review of the new head.
func (h *IssueHandler) fixReviewFindings(owner, repo string, pr *sdk.PullRequest, findings string, rounds int) {
	prNum := pr.Index
	if max := h.Cfg.Issue.Webhook.ReviewFixMaxPerPR; rounds >= max {
		fmt.Printf("[reviewer] Review-fix budget exhausted for PR #%d (%d rounds), needs human\n", prNum, rounds)
		_ = h.Developer.AddIssueLabel(owner, repo, prNum, "status:needs_human")
		_, _ = h.Reviewer.CreateIssueComment(owner, repo, prNum,
			"## Review loop stopped\n\nThe developer agent could not satisfy the review within the allowed rounds. Human attention required.")
		_ = upsertStatus(h.Developer, owner, repo, prNum, func(s *workStatus) {
			s.Escalated = true
		})
		return
	}

	fmt.Printf("[reviewer] REQUEST_CHANGES on PR #%d, launching developer (round %d)\n", prNum, rounds+1)
	branch := pr.Head.Ref
	localBranch := fmt.Sprintf("fix/pr-%d", prNum)
	path := h.worktreePath(fmt.Sprintf("pr-%d", prNum))
	if err := h.ensureWorktree(localBranch, path, "origin/"+branch); err != nil {
		fmt.Printf("[reviewer] Failed to prepare fix worktree for PR #%d: %v\n", prNum, err)
		return
	}

	promptPath := filepath.Join(path, ".issue-agent-prompt.md")
	prompt := fmt.Sprintf(`# Address review findings on PR #%d

Title: %s

## Task
The code reviewer requested changes on this PR. Address every finding below
with minimal, scoped changes. Then run
./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh and fix any failures. Commit.

## Review findings (all must be addressed)
%s

## Constraints (inviolable, see docs/skills/engineering-principles.md)
- Address the findings only; no unrelated changes
- Reuse existing packages and libraries before writing new code
- No mock-based or isolated unit tests
`, prNum, pr.Title, findings)
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return
	}

	if out, err := h.runAgent(path, promptPath, h.Developer); err != nil {
		fmt.Printf("[reviewer] Review-fix agent failed for PR #%d: %v: %s\n", prNum, err, out)
		return
	}
	if out, err := h.runChecks(path); err != nil {
		fmt.Printf("[reviewer] Checks failing after review fix on PR #%d: %s\n", prNum, out)
		return
	}
	if err := h.commitFix(path, localBranch, branch, "fix(review): address reviewer findings"); err != nil {
		fmt.Printf("[reviewer] Failed to push review fix for PR #%d: %v\n", prNum, err)
		return
	}
	_ = upsertStatus(h.Developer, owner, repo, prNum, func(s *workStatus) {
		s.ReviewFixes++
		s.Stage = stageDeveloper
	})
	fmt.Printf("[reviewer] Pushed review fix for PR #%d\n", prNum)
}
