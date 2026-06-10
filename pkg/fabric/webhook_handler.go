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
	"strconv"
	"strings"

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
// "Closes #N" gets the terminal completed status.
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
	fmt.Printf("[webhook] No open PR found for failed sha %s, ignoring\n", payload.SHA)
}

// FixPullRequest lets the developer agent fix a CI failure on a PR, within a
// bounded budget; when the budget is exhausted the architect reviews the design
// once and the PR is handed over to a human.
func (h *IssueHandler) FixPullRequest(owner, repo string, prNum int64, headSHA string) {
	if !h.tryClaim(h.busyPRs, prNum) {
		fmt.Printf("[fixer] PR #%d is already being processed, skipping\n", prNum)
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

	state := loadStatus(h.Developer, owner, repo, prNum)
	cfg := h.Cfg.Issue.Webhook
	if state.Escalated {
		return
	}
	if state.CIFix[headSHA] >= cfg.CIFixMaxPerSHA || state.totalCIFixes() >= cfg.CIFixMaxPerPR {
		h.escalateToArchitect(owner, repo, pr)
		return
	}

	// A distinct local branch avoids "already checked out" conflicts with the
	// issue worktree that owns the PR's head branch.
	branch := pr.Head.Ref
	localBranch := fmt.Sprintf("fix/pr-%d", prNum)
	path := h.worktreePath(fmt.Sprintf("pr-%d", prNum))
	if err := h.ensureWorktree(localBranch, path, "origin/"+branch); err != nil {
		fmt.Printf("[fixer] Failed to prepare worktree for PR #%d: %v\n", prNum, err)
		return
	}

	// Reproduce the failure locally; the check output is the fix context.
	checkOut, checkErr := h.runChecks(path)
	if checkErr == nil {
		fmt.Printf("[fixer] Checks pass locally for PR #%d, nothing to fix\n", prNum)
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

	if err := h.commitFix(path, localBranch, branch); err != nil {
		fmt.Printf("[fixer] Failed to push fix for PR #%d: %v\n", prNum, err)
		return
	}

	// Budget is consumed only by a pushed fix: a pushed commit re-triggers CI
	// and may loop, while failed attempts terminate on their own.
	_ = upsertStatus(h.Developer, owner, repo, prNum, func(s *workStatus) {
		if s.CIFix == nil {
			s.CIFix = map[string]int{}
		}
		s.CIFix[headSHA]++
		s.Stage = stageDeveloper
	})
	fmt.Printf("[fixer] Pushed fix for PR #%d\n", prNum)
}

func (h *IssueHandler) commitFix(path, localBranch, remoteBranch string) error {
	status, err := h.runCommand(path, nil, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		if _, err := h.runCommand(path, nil, "git", "add", "."); err != nil {
			return err
		}
		if out, err := h.runCommand(path, h.gitIdentityEnv(), "git", "commit", "-m", "fix(ci): repair failing checks"); err != nil {
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

// ReviewPullRequest posts a single automated review per PR head SHA.
func (h *IssueHandler) ReviewPullRequest(owner, repo string, prNum int64) {
	if !h.tryClaim(h.busyPRs, prNum) {
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

Output a concise markdown review with:
- Verdict: APPROVE or REQUEST_CHANGES
- Findings: bullet list, each with file reference, only real issues

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
}
