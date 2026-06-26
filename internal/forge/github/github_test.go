package github

import (
	"encoding/json"
	"testing"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

func TestGhPRToDomain(t *testing.T) {
	var p ghPR
	raw := `{"number":7,"title":"T","state":"OPEN","headRefName":"feat","baseRefName":"main",
	         "headRefOid":"abc123","mergeable":"MERGEABLE","author":{"login":"me"}}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	d := p.toDomain()
	if d.Number != 7 || d.State != domain.PRStateOpen || d.HeadRef != "feat" || d.BaseRef != "main" || d.HeadSHA != "abc123" || d.Author != "me" {
		t.Errorf("mapping = %+v", d)
	}
	if d.Mergeable == nil || !*d.Mergeable {
		t.Errorf("mergeable = %v", d.Mergeable)
	}
}

func TestMapBucket(t *testing.T) {
	cases := map[string]domain.CheckBucket{
		"pass": domain.CheckPass, "fail": domain.CheckFail, "skipping": domain.CheckSkip,
		"cancel": domain.CheckCancel, "pending": domain.CheckPending, "weird": domain.CheckPending,
	}
	for in, want := range cases {
		if got := mapBucket(in); got != want {
			t.Errorf("mapBucket(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestGithubHelpers(t *testing.T) {
	if !isNumeric("123") || isNumeric("12a") || isNumeric("") {
		t.Errorf("isNumeric")
	}
	if prNumberFromURL("https://github.com/o/r/pull/42") != 42 {
		t.Errorf("prNumberFromURL")
	}
	if prNumberFromURL("nonsense") != 0 {
		t.Errorf("prNumberFromURL nonsense should be 0")
	}
	if got := lastNonEmptyLine("warn\n\n  https://x/pull/1  \n"); got != "https://x/pull/1" {
		t.Errorf("lastNonEmptyLine = %q", got)
	}
}
