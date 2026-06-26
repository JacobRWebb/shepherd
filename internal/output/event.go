package output

// Event is one streaming output record. In JSON mode it is emitted as a single
// NDJSON line; in human mode it renders as a short progress line.
type Event struct {
	Kind string `json:"kind"`
	Time string `json:"ts,omitempty"`
	Data any    `json:"data,omitempty"`
}
