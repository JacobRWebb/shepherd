package agent

import (
	"bytes"
	"encoding/json"
	"strings"
)

// HeadlessResult is the parsed outcome of a `claude -p` run. Raw retains the full
// envelope so we stay resilient to claude output-shape changes.
type HeadlessResult struct {
	SessionID  string          `json:"session_id,omitempty"`
	Text       string          `json:"text"`
	IsError    bool            `json:"is_error"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	CostUSD    float64         `json:"cost_usd,omitempty"`
	NumTurns   int             `json:"num_turns,omitempty"`
	ExitCode   int             `json:"exit_code"`
	Stderr     string          `json:"stderr,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

// StreamEvent is one line of `--output-format stream-json` output.
type StreamEvent struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

func trimSpace(s string) string { return strings.TrimSpace(s) }

// parseHeadless leniently parses claude's --output-format json result envelope.
// Unknown/missing fields are tolerated; on any parse failure the raw text is
// surfaced as Text.
func parseHeadless(b []byte, format string) HeadlessResult {
	res := HeadlessResult{Raw: append(json.RawMessage(nil), b...)}
	b = bytes.TrimSpace(b)
	if format == "text" || len(b) == 0 || b[0] != '{' {
		res.Text = strings.TrimSpace(string(b))
		return res
	}
	var env struct {
		Type       string  `json:"type"`
		Subtype    string  `json:"subtype"`
		IsError    bool    `json:"is_error"`
		DurationMS int64   `json:"duration_ms"`
		NumTurns   int     `json:"num_turns"`
		Result     string  `json:"result"`
		SessionID  string  `json:"session_id"`
		TotalCost  float64 `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		res.Text = strings.TrimSpace(string(b))
		return res
	}
	res.Text = env.Result
	res.IsError = env.IsError || strings.HasPrefix(env.Subtype, "error")
	res.DurationMS = env.DurationMS
	res.NumTurns = env.NumTurns
	res.SessionID = env.SessionID
	res.CostUSD = env.TotalCost
	return res
}
