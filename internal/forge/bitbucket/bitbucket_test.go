package bitbucket

import (
	"encoding/json"
	"testing"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

func TestBBPRToDomain(t *testing.T) {
	var p bbPR
	raw := `{"id":5,"title":"T","state":"MERGED",
	         "source":{"branch":{"name":"feat"},"commit":{"hash":"deadbeef"}},
	         "destination":{"branch":{"name":"main"}},
	         "author":{"display_name":"Me"},"links":{"html":{"href":"http://x"}}}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	d := p.toDomain()
	if d.Number != 5 || d.State != domain.PRStateMerged || d.HeadRef != "feat" || d.HeadSHA != "deadbeef" || d.URL != "http://x" {
		t.Errorf("mapping = %+v", d)
	}
}

func TestMapBBState(t *testing.T) {
	cases := map[string]domain.CheckBucket{
		"SUCCESSFUL": domain.CheckPass, "FAILED": domain.CheckFail,
		"INPROGRESS": domain.CheckPending, "STOPPED": domain.CheckCancel,
	}
	for in, want := range cases {
		if got := mapBBState(in); got != want {
			t.Errorf("mapBBState(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBBCommentInline(t *testing.T) {
	var c bbComment
	raw := `{"id":1,"content":{"raw":"fix this"},"user":{"display_name":"R"},"inline":{"path":"main.go","to":12}}`
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	d := c.toDomain()
	if !d.IsReview || d.Path != "main.go" || d.Line != 12 || d.Body != "fix this" {
		t.Errorf("mapping = %+v", d)
	}
}
