// Package stats aggregates agent usage statistics (cost, turns, and token
// counts) by reading the per-session log files written by the session backends
// and parsing the claude JSON result envelope found in each. Per-model figures
// come from the envelope's modelUsage map (keyed by model name).
package stats

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"sort"

	"github.com/JacobRWebb/shepherd/internal/session"
)

// modelUnknown labels usage from envelopes that omit per-model data.
const modelUnknown = "unknown"

// Usage mirrors the cumulative usage object in the claude result envelope.
type Usage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
}

// modelUsageEntry mirrors one value in the envelope's modelUsage map.
type modelUsageEntry struct {
	InputTokens         int     `json:"inputTokens"`
	OutputTokens        int     `json:"outputTokens"`
	CacheCreationTokens int     `json:"cacheCreationInputTokens"`
	CacheReadTokens     int     `json:"cacheReadInputTokens"`
	CostUSD             float64 `json:"costUSD"`
}

// envelope is the (leniently parsed) claude `--output-format json` result.
type envelope struct {
	Type       string                     `json:"type"`
	TotalCost  float64                    `json:"total_cost_usd"`
	NumTurns   int                        `json:"num_turns"`
	Usage      Usage                      `json:"usage"`
	ModelUsage map[string]modelUsageEntry `json:"modelUsage"`
}

// Totals are the grand aggregates across every session.
type Totals struct {
	Runs                int     `json:"runs"`
	Turns               int     `json:"turns"`
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheCreationTokens int     `json:"cache_creation_input_tokens"`
	CacheReadTokens     int     `json:"cache_read_input_tokens"`
	CostUSD             float64 `json:"cost_usd"`
}

// ModelStat is the per-model breakdown.
type ModelStat struct {
	Model        string  `json:"model"`
	Runs         int     `json:"runs"`
	Turns        int     `json:"turns"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// Report is the aggregated usage statistics.
type Report struct {
	Totals  Totals      `json:"totals"`
	ByModel []ModelStat `json:"by_model"`
}

// Collect reads every session's log file from store, parses the claude result
// envelope, and aggregates grand totals plus a per-model breakdown. Sessions
// without a readable log (or without a parseable envelope) are skipped.
func Collect(store *session.Store) (Report, error) {
	sessions, err := store.All()
	if err != nil {
		return Report{}, err
	}

	var report Report
	byModel := map[string]*ModelStat{}
	modelStat := func(name string) *ModelStat {
		m := byModel[name]
		if m == nil {
			m = &ModelStat{Model: name}
			byModel[name] = m
		}
		return m
	}

	for _, s := range sessions {
		if s.LogPath == "" {
			continue
		}
		data, rerr := os.ReadFile(s.LogPath)
		if rerr != nil {
			continue
		}
		env, ok := parseLog(data)
		if !ok {
			continue
		}

		report.Totals.Runs++
		report.Totals.Turns += env.NumTurns
		report.Totals.InputTokens += env.Usage.InputTokens
		report.Totals.OutputTokens += env.Usage.OutputTokens
		report.Totals.CacheCreationTokens += env.Usage.CacheCreationTokens
		report.Totals.CacheReadTokens += env.Usage.CacheReadTokens
		report.Totals.CostUSD += env.TotalCost

		if len(env.ModelUsage) == 0 {
			// No per-model data; attribute the run's usage to "unknown".
			m := modelStat(modelUnknown)
			m.Runs++
			m.Turns += env.NumTurns
			m.InputTokens += env.Usage.InputTokens
			m.OutputTokens += env.Usage.OutputTokens
			m.CostUSD += env.TotalCost
			continue
		}
		for name, mu := range env.ModelUsage {
			m := modelStat(name)
			m.Runs++
			m.Turns += env.NumTurns // run-level; a run may span multiple models
			m.InputTokens += mu.InputTokens
			m.OutputTokens += mu.OutputTokens
			m.CostUSD += mu.CostUSD
		}
	}

	report.ByModel = make([]ModelStat, 0, len(byModel))
	for _, m := range byModel {
		report.ByModel = append(report.ByModel, *m)
	}
	sort.Slice(report.ByModel, func(i, j int) bool {
		return report.ByModel[i].Model < report.ByModel[j].Model
	})
	return report, nil
}

// parseLog locates the claude result envelope inside a log file. It scans
// line-by-line (stream-json / NDJSON), preferring a record of type "result",
// then falls back to parsing the whole file as a single JSON object.
func parseLog(data []byte) (envelope, bool) {
	var found envelope
	ok := false

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var env envelope
		if jerr := json.Unmarshal(line, &env); jerr != nil {
			continue
		}
		if env.Type == "result" {
			return env, true
		}
		if env.TotalCost != 0 || env.NumTurns != 0 || len(env.ModelUsage) > 0 {
			found, ok = env, true
		}
	}
	if ok {
		return found, true
	}

	var env envelope
	if jerr := json.Unmarshal(bytes.TrimSpace(data), &env); jerr == nil {
		return env, true
	}
	return envelope{}, false
}
