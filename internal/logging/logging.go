// Package logging configures the process-wide zerolog logger.
//
// Diagnostics always go to stderr so they never corrupt machine-readable output
// on stdout (see internal/output). When a file sink is configured it always
// receives raw JSON, regardless of the stderr format.
package logging

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New builds the logger. jsonFormat true => structured JSON on stderr; false =>
// a colored console writer. level is debug|info|warn|error (defaults to info).
// A non-empty file adds a second, always-JSON sink.
func New(level string, jsonFormat, noColor bool, file string) (*zerolog.Logger, error) {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}

	var sink io.Writer = os.Stderr
	if !jsonFormat {
		sink = zerolog.ConsoleWriter{Out: os.Stderr, NoColor: noColor, TimeFormat: time.Kitchen}
	}

	if file != "" {
		f, ferr := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if ferr != nil {
			return nil, ferr
		}
		// MultiLevelWriter feeds the logger's JSON bytes to each sink. The
		// ConsoleWriter reformats them for stderr; the file keeps raw JSON.
		sink = zerolog.MultiLevelWriter(sink, f)
	}

	l := zerolog.New(sink).Level(lvl).With().Timestamp().Logger()
	return &l, nil
}

// Nop returns a disabled logger, handy for tests.
func Nop() *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}
