package domain

import (
	"regexp"
	"strings"
)

// TaskSource records where a Task came from.
type TaskSource string

const (
	TaskSourceFreeText TaskSource = "free_text"
	TaskSourceIssue    TaskSource = "issue"
)

// Task is a unit of work to be carried out inside a worktree. It is derived
// either from a forge issue reference (#123, PROJ-45, a URL) or from free text.
type Task struct {
	Raw     string     `json:"raw"`                // original user input
	Title   string     `json:"title"`              // short title (issue title or first line)
	Body    string     `json:"body,omitempty"`     // detailed description (issue body or full text)
	Source  TaskSource `json:"source"`             // free_text | issue
	IssueID string     `json:"issue_id,omitempty"` // set when Source == TaskSourceIssue (e.g. "123")
}

var (
	nonSlug     = regexp.MustCompile(`[^a-z0-9]+`)
	issueRefPat = regexp.MustCompile(`^(?:#(\d+)|([A-Za-z][A-Za-z0-9]*-\d+))$`)
)

// NewTask interprets raw as either an issue reference or free text. Forge
// enrichment (fetching the real title/body) happens later in the `new` flow;
// this only does cheap local parsing so the tool works offline.
func NewTask(raw string) Task {
	raw = strings.TrimSpace(raw)
	t := Task{Raw: raw, Source: TaskSourceFreeText, Title: firstLine(raw)}
	if m := issueRefPat.FindStringSubmatch(raw); m != nil {
		t.Source = TaskSourceIssue
		if m[1] != "" {
			t.IssueID = m[1] // "#123" -> "123"
		} else {
			t.IssueID = m[2] // "PROJ-45"
		}
		t.Title = raw
	}
	return t
}

// Slug returns a lowercase, dash-separated identifier derived from the task,
// suitable as the base for a branch/worktree name. It is OS-agnostic; the
// worktree package applies any filesystem sanitization on top.
func (t Task) Slug() string {
	base := t.Title
	if base == "" {
		base = t.Raw
	}
	slug := sanitizeSlug(base)
	if t.IssueID != "" {
		if p := sanitizeSlug(t.IssueID); p != "" && !strings.HasPrefix(slug, p) {
			slug = p + "-" + slug
		}
	}
	if slug == "" {
		slug = "task"
	}
	return slug
}

func sanitizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlug.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = strings.Trim(s[:50], "-")
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
