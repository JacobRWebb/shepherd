// Package output renders command results to stdout, choosing human text or JSON
// based on the --json flag. It is the analog of an HTTP response layer: the one
// place that decides how a result is shaped for the caller.
//
// Golden rule: stdout is for machine/result output, stderr is for diagnostics
// (the logger). Never mix the two.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

// Writer renders results either as human text or JSON.
type Writer struct {
	json bool
	out  io.Writer
}

// New builds a Writer. Pass os.Stdout for out.
func New(jsonMode bool, out io.Writer) *Writer {
	return &Writer{json: jsonMode, out: out}
}

// JSON reports whether the writer is in JSON mode (so callers can suppress
// chatty human-only output).
func (w *Writer) JSON() bool { return w.json }

// Result renders a successful result. JSON: {"ok":true,"data":<v>} (pretty).
// Human: prints render() if non-nil.
func (w *Writer) Result(v any, render func() string) {
	if w.json {
		w.encodePretty(map[string]any{"ok": true, "data": v})
		return
	}
	if render != nil {
		if s := render(); s != "" {
			fmt.Fprintln(w.out, s)
		}
	}
}

// Error renders a failure. JSON: {"ok":false,"error":{code,message}}. Human:
// "error: <message>". The code is derived from the wrapped domain sentinel.
func (w *Writer) Error(err error) {
	if err == nil {
		return
	}
	if w.json {
		w.encodePretty(map[string]any{
			"ok": false,
			"error": map[string]string{
				"code":    classify(err),
				"message": err.Error(),
			},
		})
		return
	}
	fmt.Fprintln(w.out, "error:", err.Error())
}

// Event emits one streaming record. JSON: a compact NDJSON line. Human: a short
// bullet line.
func (w *Writer) Event(kind string, v any) {
	if w.json {
		b, _ := json.Marshal(Event{Kind: kind, Time: time.Now().UTC().Format(time.RFC3339), Data: v})
		_, _ = w.out.Write(append(b, '\n'))
		return
	}
	if v != nil {
		fmt.Fprintf(w.out, "• %s: %v\n", kind, v)
	} else {
		fmt.Fprintf(w.out, "• %s\n", kind)
	}
}

// Table renders rows. JSON mode emits jsonRows via Result; human mode prints an
// aligned table.
func (w *Writer) Table(headers []string, rows [][]string, jsonRows any) {
	if w.json {
		w.Result(jsonRows, nil)
		return
	}
	tw := tabwriter.NewWriter(w.out, 0, 2, 2, ' ', 0)
	if len(headers) > 0 {
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
	}
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	_ = tw.Flush()
}

func (w *Writer) encodePretty(v any) {
	enc := json.NewEncoder(w.out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// classify maps a (possibly wrapped) domain sentinel to a stable error code.
func classify(err error) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return "not_found"
	case errors.Is(err, domain.ErrInvalidInput):
		return "invalid_input"
	case errors.Is(err, domain.ErrConflict):
		return "conflict"
	case errors.Is(err, domain.ErrNotGitRepo):
		return "not_git_repo"
	case errors.Is(err, domain.ErrDirty):
		return "dirty"
	case errors.Is(err, domain.ErrUnsupported):
		return "unsupported"
	default:
		return "internal"
	}
}

// ExitCode maps a (possibly wrapped) domain sentinel to a process exit code.
// Kept here so the CLI and the output layer agree on the mapping.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, domain.ErrInvalidInput):
		return 2
	case errors.Is(err, domain.ErrNotFound):
		return 3
	case errors.Is(err, domain.ErrNotGitRepo):
		return 4
	default:
		return 1
	}
}
