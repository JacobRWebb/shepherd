package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Build metadata, overridable via -ldflags
// "-X github.com/JacobRWebb/shepherd/internal/cli.version=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// effectiveVersion returns the ldflags-injected version (release builds), or
// falls back to the module version embedded by `go install` (debug.BuildInfo),
// then "dev" for plain local builds.
func effectiveVersion() string {
	if version != "dev" && version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

// renderVersion renders the human-readable version output. When short is true it
// returns only the bare version string; otherwise the full build/runtime line.
func renderVersion(short bool, v string) string {
	if short {
		return v
	}
	return fmt.Sprintf("shepherd %s (commit %s, built %s, %s %s/%s)",
		v, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := stateFrom(cmd)
			short, _ := cmd.Flags().GetBool("short")
			v := effectiveVersion()
			info := map[string]string{
				"version": v,
				"commit":  commit,
				"date":    date,
				"go":      runtime.Version(),
				"os":      runtime.GOOS,
				"arch":    runtime.GOARCH,
			}
			st.Out.Result(info, func() string {
				return renderVersion(short, v)
			})
			return nil
		},
	}
	cmd.Flags().Bool("short", false, "print only the bare version string")
	return cmd
}
