package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build metadata, overridable via -ldflags
// "-X github.com/JacobRWebb/shepherd/internal/cli.version=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := stateFrom(cmd)
			info := map[string]string{
				"version": version,
				"commit":  commit,
				"date":    date,
				"go":      runtime.Version(),
				"os":      runtime.GOOS,
				"arch":    runtime.GOARCH,
			}
			st.Out.Result(info, func() string {
				return fmt.Sprintf("shepherd %s (commit %s, built %s, %s %s/%s)",
					version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			})
			return nil
		},
	}
}
