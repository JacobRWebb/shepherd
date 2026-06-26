package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/updater"
)

// repoSlug is the GitHub repository self-update pulls releases from.
const repoSlug = "JacobRWebb/shepherd"

func newUpdateCmd() *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update shepherd to the latest release",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := stateFrom(cmd)
			res, err := updater.Update(cmd.Context(), repoSlug, effectiveVersion(), checkOnly)
			if err != nil {
				return err
			}
			st.Out.Result(res, func() string {
				switch {
				case res.UpToDate:
					return fmt.Sprintf("shepherd is up to date (%s)", res.Current)
				case res.CheckOnly:
					return fmt.Sprintf("update available: %s → %s (run `shepherd update`)", display(res.Current), res.Latest)
				case res.Updated:
					return fmt.Sprintf("updated %s → %s", display(res.Current), res.Latest)
				default:
					return "no update performed"
				}
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check for an update; do not install")
	return cmd
}

func display(v string) string {
	if v == "" || v == "dev" {
		return "dev"
	}
	return v
}

// maybeNotifyUpdate prints an unobtrusive "update available" notice on stderr
// after a successful command. It is cached (24h) and skipped for machine output,
// dev builds, the update/version/tui commands, and when SHEPHERD_NO_UPDATE_CHECK
// is set.
func maybeNotifyUpdate(cmd *cobra.Command) {
	if os.Getenv("SHEPHERD_NO_UPDATE_CHECK") != "" {
		return
	}
	switch cmd.Name() {
	case "shepherd", "tui", "update", "version", "help":
		return
	}
	st := stateFrom(cmd)
	if st == nil || st.JSON {
		return
	}
	info := updater.CachedCheck(cmd.Context(), repoSlug, effectiveVersion())
	if !info.Available {
		return
	}
	msg := fmt.Sprintf("Update available: %s → %s\nRun `shepherd update` to upgrade.", display(info.Current), info.Latest)
	if flagNoColor {
		fmt.Fprintf(os.Stderr, "\n%s\n", msg)
		return
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("11")).
		Padding(0, 1).
		Render(msg)
	fmt.Fprintln(os.Stderr, "\n"+box)
}
