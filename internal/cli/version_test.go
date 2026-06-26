package cli

import (
	"runtime"
	"strings"
	"testing"
)

func TestVersionLine(t *testing.T) {
	const v = "1.2.3"

	t.Run("short prints only the bare version string", func(t *testing.T) {
		if got := versionLine(true, v); got != v {
			t.Fatalf("versionLine(true, %q) = %q, want %q", v, got, v)
		}
	})

	t.Run("long form includes the runtime info", func(t *testing.T) {
		got := versionLine(false, v)
		if !strings.HasPrefix(got, "shepherd "+v) {
			t.Errorf("long form = %q, want prefix %q", got, "shepherd "+v)
		}
		if !strings.Contains(got, runtime.Version()) {
			t.Errorf("long form = %q, want it to contain %q", got, runtime.Version())
		}
		if !strings.Contains(got, runtime.GOOS+"/"+runtime.GOARCH) {
			t.Errorf("long form = %q, want it to contain %q", got, runtime.GOOS+"/"+runtime.GOARCH)
		}
	})
}
