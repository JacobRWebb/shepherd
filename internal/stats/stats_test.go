package stats

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JacobRWebb/shepherd/internal/session"
)

// sampleEnvelope is a representative claude `--output-format json` result. The
// per-model breakdown comes from modelUsage; the grand totals from the top-level
// usage + total_cost_usd.
const sampleEnvelope = `{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "num_turns": 7,
  "total_cost_usd": 0.1234,
  "usage": {
    "input_tokens": 100,
    "output_tokens": 200,
    "cache_creation_input_tokens": 30,
    "cache_read_input_tokens": 40
  },
  "modelUsage": {
    "claude-opus-4-8": {"inputTokens":100,"outputTokens":200,"cacheCreationInputTokens":30,"cacheReadInputTokens":40,"costUSD":0.1234}
  }
}`

// streamEnvelope is the same shape delivered as stream-json (NDJSON) with a
// preceding non-result line that must be ignored.
const streamEnvelope = `{"type":"assistant","message":"working"}
{"type":"result","num_turns":3,"total_cost_usd":0.05,"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":1,"cache_read_input_tokens":2},"modelUsage":{"claude-opus-4-8":{"inputTokens":10,"outputTokens":20,"costUSD":0.05}}}`

// haikuEnvelope exercises the per-model breakdown with a different model.
const haikuEnvelope = `{"type":"result","num_turns":2,"total_cost_usd":0.01,"usage":{"input_tokens":5,"output_tokens":6,"cache_creation_input_tokens":0,"cache_read_input_tokens":0},"modelUsage":{"claude-haiku-4-5":{"inputTokens":5,"outputTokens":6,"costUSD":0.01}}}`

func writeLog(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return p
}

func TestCollect(t *testing.T) {
	dir := t.TempDir()
	store, err := session.OpenStore(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	for _, s := range []session.Info{
		{Name: "a", LogPath: writeLog(t, dir, "a.log", sampleEnvelope)},
		{Name: "b", LogPath: writeLog(t, dir, "b.log", streamEnvelope)},
		{Name: "c", LogPath: writeLog(t, dir, "c.log", haikuEnvelope)},
		// No log path: must be skipped, not counted as a run.
		{Name: "d"},
		// Unparseable log: must be skipped.
		{Name: "e", LogPath: writeLog(t, dir, "e.log", "not json")},
	} {
		if err := store.Upsert(s); err != nil {
			t.Fatalf("upsert %s: %v", s.Name, err)
		}
	}

	report, err := Collect(store)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	// Grand totals: 3 valid runs (a + b + c).
	want := Totals{
		Runs:                3,
		Turns:               7 + 3 + 2,
		InputTokens:         100 + 10 + 5,
		OutputTokens:        200 + 20 + 6,
		CacheCreationTokens: 30 + 1 + 0,
		CacheReadTokens:     40 + 2 + 0,
		CostUSD:             0.1234 + 0.05 + 0.01,
	}
	if report.Totals.Runs != want.Runs {
		t.Errorf("Runs = %d, want %d", report.Totals.Runs, want.Runs)
	}
	if report.Totals.Turns != want.Turns {
		t.Errorf("Turns = %d, want %d", report.Totals.Turns, want.Turns)
	}
	if report.Totals.InputTokens != want.InputTokens {
		t.Errorf("InputTokens = %d, want %d", report.Totals.InputTokens, want.InputTokens)
	}
	if report.Totals.OutputTokens != want.OutputTokens {
		t.Errorf("OutputTokens = %d, want %d", report.Totals.OutputTokens, want.OutputTokens)
	}
	if report.Totals.CacheCreationTokens != want.CacheCreationTokens {
		t.Errorf("CacheCreationTokens = %d, want %d", report.Totals.CacheCreationTokens, want.CacheCreationTokens)
	}
	if report.Totals.CacheReadTokens != want.CacheReadTokens {
		t.Errorf("CacheReadTokens = %d, want %d", report.Totals.CacheReadTokens, want.CacheReadTokens)
	}
	if diff := report.Totals.CostUSD - want.CostUSD; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("CostUSD = %v, want %v", report.Totals.CostUSD, want.CostUSD)
	}

	// Per-model breakdown: sorted by model name (haiku before opus).
	if len(report.ByModel) != 2 {
		t.Fatalf("ByModel len = %d, want 2: %+v", len(report.ByModel), report.ByModel)
	}
	if report.ByModel[0].Model != "claude-haiku-4-5" {
		t.Errorf("ByModel[0].Model = %q, want claude-haiku-4-5", report.ByModel[0].Model)
	}
	opus := report.ByModel[1]
	if opus.Model != "claude-opus-4-8" {
		t.Fatalf("ByModel[1].Model = %q, want claude-opus-4-8", opus.Model)
	}
	if opus.Runs != 2 {
		t.Errorf("opus Runs = %d, want 2", opus.Runs)
	}
	if opus.Turns != 10 {
		t.Errorf("opus Turns = %d, want 10", opus.Turns)
	}
	if opus.InputTokens != 110 {
		t.Errorf("opus InputTokens = %d, want 110", opus.InputTokens)
	}
	if opus.OutputTokens != 220 {
		t.Errorf("opus OutputTokens = %d, want 220", opus.OutputTokens)
	}
}
