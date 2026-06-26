package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive dashboard",
		RunE:  runTUI,
	}
}

func runTUI(cmd *cobra.Command, _ []string) error {
	st := stateFrom(cmd)
	if st.JSON {
		return fmt.Errorf("the TUI is interactive and cannot be used with --json")
	}
	a, err := st.App()
	if err != nil {
		return err
	}
	return tui.Run(cmd.Context(), tui.Deps{
		Worktrees: a.Worktrees,
		Sessions:  a.Sessions,
		Log:       a.Log,
		Version:   version,
		Repo:      repoSlug,
	})
}
