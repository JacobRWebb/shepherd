package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/JacobRWebb/shepherd/internal/output"
)

func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"git version 2.45.0\nextra": "git version 2.45.0",
		"\n\n  tmux 3.4  \n":        "tmux 3.4",
		"":                          "",
		"   ":                       "",
		"single":                    "single",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSelectedSessionBackend(t *testing.T) {
	got := selectedSessionBackend(true)
	if runtime.GOOS == "windows" {
		if got != "native" {
			t.Errorf("on windows with tmux present, got %q, want native", got)
		}
	} else if got != "tmux" {
		t.Errorf("on %s with tmux present, got %q, want tmux", runtime.GOOS, got)
	}

	if got := selectedSessionBackend(false); got != "native" {
		t.Errorf("without tmux, got %q, want native", got)
	}
}

func TestToolRow(t *testing.T) {
	if r := toolRow(toolReport{Name: "git", Missing: true}); r.Status != "missing" {
		t.Errorf("missing tool status = %q, want missing", r.Status)
	}
	if r := toolRow(toolReport{Name: "git", Found: true, Version: "git version 2.45.0"}); r.Status != "ok" || r.Detail != "git version 2.45.0" {
		t.Errorf("found tool row = %+v", r)
	}
	if r := toolRow(toolReport{Name: "git", Found: true, Error: "boom"}); r.Status != "error" {
		t.Errorf("errored tool status = %q, want error", r.Status)
	}
}

func TestGHAuthRow(t *testing.T) {
	if r := ghAuthRow(ghAuthReport{Available: false, Detail: "gh not found on PATH"}); r.Status != "missing" {
		t.Errorf("unavailable gh status = %q, want missing", r.Status)
	}
	if r := ghAuthRow(ghAuthReport{Available: true, Authenticated: false}); r.Status != "not authenticated" {
		t.Errorf("unauth gh status = %q, want not authenticated", r.Status)
	}
	if r := ghAuthRow(ghAuthReport{Available: true, Authenticated: true}); r.Status != "ok" {
		t.Errorf("auth gh status = %q, want ok", r.Status)
	}
}

func TestBuildDoctorReport(t *testing.T) {
	rep := buildDoctorReport(context.Background())

	if len(rep.Tools) != 4 {
		t.Fatalf("expected 4 tool reports, got %d", len(rep.Tools))
	}
	if rep.Runtime["go"] != runtime.Version() ||
		rep.Runtime["os"] != runtime.GOOS ||
		rep.Runtime["arch"] != runtime.GOARCH {
		t.Errorf("runtime info mismatch: %+v", rep.Runtime)
	}
	if rep.SessionBackend != "tmux" && rep.SessionBackend != "native" {
		t.Errorf("unexpected session backend %q", rep.SessionBackend)
	}

	// Rows must cover every check category.
	want := map[string]bool{
		"runtime": true, "config": true, "session backend": true, "gh auth": true,
		"git": true, "gh": true, "claude": true, "tmux": true,
	}
	for _, r := range rep.Rows {
		delete(want, r.Check)
	}
	if len(want) != 0 {
		t.Errorf("missing rows for: %v", want)
	}
}

func TestDoctorCmdRunReturnsNilAndWritesJSON(t *testing.T) {
	var buf bytes.Buffer
	st := &cmdState{
		Out:  output.New(true, &buf),
		JSON: true,
		ctx:  context.Background(),
	}
	cmd := newDoctorCmd()
	cmd.SetContext(context.WithValue(context.Background(), ctxKey{}, st))

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("doctor RunE returned error: %v", err)
	}

	var envelope struct {
		OK   bool         `json:"ok"`
		Data doctorReport `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if !envelope.OK {
		t.Error("expected ok=true envelope")
	}
	if len(envelope.Data.Tools) != 4 {
		t.Errorf("expected 4 tools in JSON, got %d", len(envelope.Data.Tools))
	}
}

func TestDoctorCmdHumanTableOutput(t *testing.T) {
	var buf bytes.Buffer
	st := &cmdState{
		Out: output.New(false, &buf),
		ctx: context.Background(),
	}
	cmd := newDoctorCmd()
	cmd.SetContext(context.WithValue(context.Background(), ctxKey{}, st))

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("doctor RunE returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "CHECK") || !strings.Contains(out, "STATUS") || !strings.Contains(out, "DETAIL") {
		t.Errorf("human table missing headers:\n%s", out)
	}
	if !strings.Contains(out, "runtime") {
		t.Errorf("human table missing runtime row:\n%s", out)
	}
}
