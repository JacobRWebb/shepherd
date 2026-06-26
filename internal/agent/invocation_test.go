package agent

import (
	"testing"

	"github.com/JacobRWebb/shepherd/internal/config"
)

func has(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestHeadlessArgs(t *testing.T) {
	c := &Claude{cfg: config.ClaudeConfig{PermissionMode: "default"}}
	args := c.HeadlessArgs(HeadlessSpec{
		Spec:         Spec{Prompt: "do it", SessionID: "sid", Model: "opus"},
		OutputFormat: "json",
	})
	if len(args) == 0 || args[0] != "-p" {
		t.Fatalf("expected -p first: %v", args)
	}
	if !has(args, "--output-format") || !has(args, "json") {
		t.Errorf("missing output format: %v", args)
	}
	if !has(args, "--model") || !has(args, "opus") {
		t.Errorf("missing model: %v", args)
	}
	if !has(args, "--session-id") || !has(args, "sid") {
		t.Errorf("missing session id: %v", args)
	}
	if args[len(args)-1] != "do it" {
		t.Errorf("prompt must be last: %v", args)
	}
}

func TestSkipPermissionsMutualExclusion(t *testing.T) {
	c := &Claude{cfg: config.ClaudeConfig{PermissionMode: "default"}}
	args := c.HeadlessArgs(HeadlessSpec{Spec: Spec{Prompt: "x", SkipPermissions: true}, OutputFormat: "json"})
	if !has(args, "--dangerously-skip-permissions") {
		t.Errorf("expected skip flag: %v", args)
	}
	if has(args, "--permission-mode") {
		t.Errorf("permission-mode must not appear with skip: %v", args)
	}
}

func TestStreamJSONAddsVerbose(t *testing.T) {
	c := &Claude{cfg: config.ClaudeConfig{}}
	args := c.HeadlessArgs(HeadlessSpec{Spec: Spec{Prompt: "x"}, OutputFormat: "stream-json"})
	if !has(args, "--verbose") {
		t.Errorf("stream-json requires --verbose: %v", args)
	}
}

func TestParseHeadlessJSON(t *testing.T) {
	in := []byte(`{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"s1","num_turns":2,"total_cost_usd":0.5}`)
	r := parseHeadless(in, "json")
	if r.Text != "done" || r.IsError || r.SessionID != "s1" || r.NumTurns != 2 || r.CostUSD != 0.5 {
		t.Errorf("parsed = %+v", r)
	}
}
