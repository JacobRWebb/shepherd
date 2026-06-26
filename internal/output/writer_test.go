package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

func TestResultJSON(t *testing.T) {
	var b bytes.Buffer
	New(true, &b).Result(map[string]string{"k": "v"}, nil)
	var got map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("invalid json: %v (%s)", err, b.String())
	}
	if got["ok"] != true {
		t.Errorf("ok = %v", got["ok"])
	}
	if data, _ := got["data"].(map[string]any); data["k"] != "v" {
		t.Errorf("data = %v", got["data"])
	}
}

func TestErrorJSON(t *testing.T) {
	var b bytes.Buffer
	New(true, &b).Error(domain.NotFoundf("nope"))
	var got map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["ok"] != false {
		t.Errorf("ok = %v", got["ok"])
	}
	e, _ := got["error"].(map[string]any)
	if e["code"] != "not_found" {
		t.Errorf("code = %v", e["code"])
	}
}

func TestResultHuman(t *testing.T) {
	var b bytes.Buffer
	New(false, &b).Result(nil, func() string { return "hello" })
	if strings.TrimSpace(b.String()) != "hello" {
		t.Errorf("got %q", b.String())
	}
}

func TestTableJSON(t *testing.T) {
	var b bytes.Buffer
	New(true, &b).Table([]string{"A"}, [][]string{{"1"}}, []map[string]any{{"a": 1}})
	if !strings.Contains(b.String(), `"ok"`) {
		t.Errorf("expected json envelope: %s", b.String())
	}
}

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{domain.ErrInvalidInput, 2},
		{domain.NotFoundf("x"), 3},
		{domain.ErrNotGitRepo, 4},
		{errors.New("boom"), 1},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Errorf("ExitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	if classify(domain.Conflictf("x")) != "conflict" {
		t.Errorf("conflict classify")
	}
	if classify(errors.New("x")) != "internal" {
		t.Errorf("internal classify")
	}
}
