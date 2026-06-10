package fabric

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "code.gitea.io/sdk/gitea"
	"gopkg.in/yaml.v3"

	"example.org/ai-fabric/pkg/gitea"
)

const (
	statusOpen  = "<!-- ai-fabric:status"
	statusClose = "-->"
)

// workStatus is the machine-readable state of an issue or PR, stored as YAML
// inside a hidden HTML comment of a single bot-owned status comment.
// It replaces both comment-substring attempt counting and per-event comment spam.
type workStatus struct {
	Stage    string `yaml:"stage,omitempty"`
	Attempts int    `yaml:"attempts,omitempty"`
	// ArchAttempts counts failed architect runs; bounded to avoid infinite retries.
	ArchAttempts int       `yaml:"arch_attempts,omitempty"`
	ClaimedAt    time.Time `yaml:"claimed_at,omitempty"`
	UpdatedAt    time.Time `yaml:"updated_at,omitempty"`
	PRURL        string    `yaml:"pr_url,omitempty"`
	// CIFix maps a PR head SHA to the number of automated fix attempts on it.
	CIFix map[string]int `yaml:"ci_fix,omitempty"`
	// ReviewedSHAs lists PR head SHAs that already received an automated review.
	ReviewedSHAs []string `yaml:"reviewed_shas,omitempty"`
	// ReviewFixes counts developer rounds addressing reviewer REQUEST_CHANGES.
	ReviewFixes int  `yaml:"review_fixes,omitempty"`
	// ApprovedForMerge is set when the automated reviewer APPROVEs the current
	// head; cleared on REQUEST_CHANGES. Used to merge latest base before land.
	ApprovedForMerge bool `yaml:"approved_for_merge,omitempty"`
	Escalated        bool `yaml:"escalated,omitempty"`
}

func (s *workStatus) totalCIFixes() int {
	total := 0
	for _, n := range s.CIFix {
		total += n
	}
	return total
}

func (s *workStatus) hasReviewed(sha string) bool {
	return slices.Contains(s.ReviewedSHAs, sha)
}

// render produces the status comment body: hidden YAML state plus a short
// human-readable summary.
func (s *workStatus) render() string {
	raw, err := yaml.Marshal(s)
	if err != nil {
		raw = fmt.Appendf(nil, "stage: %s\n", s.Stage)
	}

	var b strings.Builder
	b.WriteString(statusOpen + "\n")
	b.Write(raw)
	b.WriteString(statusClose + "\n\n")
	b.WriteString("### AI Fabric\n\n")
	fmt.Fprintf(&b, "- Stage: `%s`\n", s.Stage)
	if s.Attempts > 0 {
		fmt.Fprintf(&b, "- Implementation attempts: %d\n", s.Attempts)
	}
	if total := s.totalCIFixes(); total > 0 {
		fmt.Fprintf(&b, "- CI fix attempts: %d\n", total)
	}
	if s.PRURL != "" {
		fmt.Fprintf(&b, "- PR: %s\n", s.PRURL)
	}
	if s.Escalated {
		b.WriteString("- Escalated to architect, human attention required\n")
	}
	if !s.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, "- Updated: %s\n", s.UpdatedAt.UTC().Format(time.RFC3339))
	}
	return b.String()
}

func parseWorkStatus(body string) (*workStatus, bool) {
	_, after, ok := strings.Cut(body, statusOpen)
	if !ok {
		return nil, false
	}
	rest := after
	before, _, ok := strings.Cut(rest, statusClose)
	if !ok {
		return nil, false
	}
	var s workStatus
	if err := yaml.Unmarshal([]byte(before), &s); err != nil {
		return nil, false
	}
	return &s, true
}

// findStatusComment locates the bot status comment on an issue or PR.
func findStatusComment(client gitea.Client, owner, repo string, number int64) (*sdk.Comment, *workStatus) {
	comments, err := client.ListIssueComments(owner, repo, number)
	if err != nil {
		return nil, nil
	}
	for _, c := range comments {
		if s, ok := parseWorkStatus(c.Body); ok {
			return c, s
		}
	}
	return nil, nil
}

// loadStatus returns the current work status of an issue/PR, or a fresh one.
func loadStatus(client gitea.Client, owner, repo string, number int64) *workStatus {
	if _, s := findStatusComment(client, owner, repo, number); s != nil {
		return s
	}
	return &workStatus{}
}

// statusMu serializes read-modify-write cycles on status comments so
// concurrent webhook goroutines (review, CI fix, merge) cannot lose updates
// or create duplicate status comments. Single-process scope is sufficient.
var statusMu sync.Mutex

// upsertStatus applies mutate to the stored status and writes it back into the
// single editable status comment (creating it on first use).
func upsertStatus(client gitea.Client, owner, repo string, number int64, mutate func(*workStatus)) error {
	statusMu.Lock()
	defer statusMu.Unlock()

	comment, status := findStatusComment(client, owner, repo, number)
	if status == nil {
		status = &workStatus{}
	}
	mutate(status)
	status.UpdatedAt = time.Now().UTC()

	body := status.render()
	if comment != nil {
		return client.EditIssueComment(owner, repo, comment.ID, body)
	}
	_, err := client.CreateIssueComment(owner, repo, number, body)
	return err
}
