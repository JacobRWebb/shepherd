package domain

import "errors"

// Repo identifies a repository across providers.
// GitHub: Owner/Name. Bitbucket: Owner=workspace, Name=repo_slug.
type Repo struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// IsZero reports whether the repo is unset.
func (r Repo) IsZero() bool { return r.Owner == "" && r.Name == "" }

// OpenPROpts are the inputs to opening a pull request.
type OpenPROpts struct {
	Title     string
	Body      string
	Head      string // source branch
	Base      string // destination branch (empty => provider default)
	Draft     bool
	Reviewers []string // logins (GitHub) or account UUIDs (Bitbucket)
	Labels    []string // GitHub only
}

// Forge-related sentinel errors (defined here so provider impls can return them
// without importing the forge package).
var (
	// ErrUnsupportedIssueRef means the issue id cannot be resolved by this
	// provider (e.g. a non-numeric ref on GitHub).
	ErrUnsupportedIssueRef = errors.New("issue reference not supported by this provider")
	// ErrIssueTrackerDisabled means the repository has no issue tracker.
	ErrIssueTrackerDisabled = errors.New("issue tracker is disabled for this repository")
)
