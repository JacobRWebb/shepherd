package domain

import "time"

// PRState is the forge-neutral state of a pull request.
type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
	PRStateMerged PRState = "merged"
)

// CheckBucket normalizes CI/check outcomes across providers. It mirrors the
// GitHub `gh pr checks --json bucket` categories; Bitbucket states are mapped
// onto the same set.
type CheckBucket string

const (
	CheckPass    CheckBucket = "pass"
	CheckFail    CheckBucket = "fail"
	CheckPending CheckBucket = "pending"
	CheckSkip    CheckBucket = "skipping"
	CheckCancel  CheckBucket = "cancel"
)

// PullRequest is a forge-neutral pull/merge request.
type PullRequest struct {
	Number      int       `json:"number"`
	ID          string    `json:"id,omitempty"`
	Title       string    `json:"title"`
	Body        string    `json:"body,omitempty"`
	State       PRState   `json:"state"`
	IsDraft     bool      `json:"is_draft"`
	URL         string    `json:"url"`
	HeadRef     string    `json:"head_ref"`           // source branch
	BaseRef     string    `json:"base_ref"`           // destination branch
	HeadSHA     string    `json:"head_sha,omitempty"` // source commit (Bitbucket needs this)
	Author      string    `json:"author,omitempty"`
	Mergeable   *bool     `json:"mergeable,omitempty"`    // nil = unknown
	ReviewState string    `json:"review_state,omitempty"` // e.g. APPROVED, CHANGES_REQUESTED
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

// Check is a forge-neutral CI/status check on a pull request.
type Check struct {
	Name        string      `json:"name"`
	Bucket      CheckBucket `json:"bucket"`
	RawState    string      `json:"raw_state,omitempty"` // provider-native state, for logs
	Workflow    string      `json:"workflow,omitempty"`
	Link        string      `json:"link,omitempty"`
	Description string      `json:"description,omitempty"`
	StartedAt   time.Time   `json:"started_at,omitempty"`
	CompletedAt time.Time   `json:"completed_at,omitempty"`
}

// Comment is a forge-neutral PR comment. A non-empty Path marks an inline review
// comment anchored to a file/line.
type Comment struct {
	ID        string    `json:"id,omitempty"`
	Author    string    `json:"author,omitempty"`
	Body      string    `json:"body"`
	Path      string    `json:"path,omitempty"`
	Line      int       `json:"line,omitempty"`
	IsReview  bool      `json:"is_review"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// Issue is a forge-neutral issue/work item.
type Issue struct {
	ID     string   `json:"id,omitempty"`
	Number int      `json:"number,omitempty"`
	Title  string   `json:"title"`
	Body   string   `json:"body,omitempty"`
	State  string   `json:"state,omitempty"`
	Author string   `json:"author,omitempty"`
	URL    string   `json:"url,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

// CheckSummary is an aggregate verdict over a set of checks.
type CheckSummary struct {
	Checks  []Check `json:"checks"`
	AllPass bool    `json:"all_pass"`
	AnyFail bool    `json:"any_fail"`
	Pending int     `json:"pending"`
	Failed  []Check `json:"failed,omitempty"`
}

// Summarize reduces a slice of checks to a CheckSummary. With no checks,
// AllPass is false (nothing has reported success yet).
func Summarize(cs []Check) CheckSummary {
	s := CheckSummary{Checks: cs, AllPass: len(cs) > 0}
	for _, c := range cs {
		switch c.Bucket {
		case CheckFail, CheckCancel:
			s.AnyFail = true
			s.AllPass = false
			s.Failed = append(s.Failed, c)
		case CheckPending:
			s.Pending++
			s.AllPass = false
		}
	}
	return s
}
