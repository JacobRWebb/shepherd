package domain

import "testing"

func TestNewTask(t *testing.T) {
	if tk := NewTask("#123"); tk.Source != TaskSourceIssue || tk.IssueID != "123" {
		t.Errorf("github issue: %+v", tk)
	}
	if tk := NewTask("PROJ-45"); tk.Source != TaskSourceIssue || tk.IssueID != "PROJ-45" {
		t.Errorf("jira issue: %+v", tk)
	}
	if tk := NewTask("fix the thing"); tk.Source != TaskSourceFreeText {
		t.Errorf("free text: %+v", tk)
	}
}

func TestSlug(t *testing.T) {
	if s := NewTask("Fix the Login Timeout!").Slug(); s != "fix-the-login-timeout" {
		t.Errorf("slug = %q", s)
	}
	if s := NewTask("   ").Slug(); s == "" {
		t.Errorf("empty slug should fall back, got %q", s)
	}
}

func TestSummarize(t *testing.T) {
	checks := []Check{
		{Name: "a", Bucket: CheckPass},
		{Name: "b", Bucket: CheckFail},
		{Name: "c", Bucket: CheckPending},
	}
	s := Summarize(checks)
	if !s.AnyFail || s.AllPass || s.Pending != 1 || len(s.Failed) != 1 {
		t.Errorf("summary = %+v", s)
	}
}
