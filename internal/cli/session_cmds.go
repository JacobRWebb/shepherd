package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/session"
)

// newLogsCmd streams (or snapshots) a session's captured output to stdout.
func newLogsCmd() *cobra.Command {
	var follow bool
	var lines int
	cmd := &cobra.Command{
		Use:   "logs <session>",
		Short: "Show a session's output (use -f to follow)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st := stateFrom(cmd)
			a, err := st.App()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			name := args[0]

			if !follow {
				out, serr := a.Sessions.Snapshot(ctx, name, lines)
				if serr != nil {
					return serr
				}
				fmt.Fprint(os.Stdout, out)
				if out != "" && !endsWithNewline(out) {
					fmt.Fprintln(os.Stdout)
				}
				return nil
			}

			rc, serr := a.Sessions.Tail(ctx, name, true)
			if serr != nil {
				return serr
			}
			defer func() { _ = rc.Close() }()
			_, cerr := io.Copy(os.Stdout, rc)
			return cerr
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&follow, "follow", "f", false, "follow the log output (tail -f)")
	f.IntVarP(&lines, "lines", "n", 200, "number of trailing lines to show when not following")
	return cmd
}

// newAttachCmd connects the terminal to a live session, falling back to a hint
// for backends (e.g. native) that cannot attach.
func newAttachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach <session>",
		Short: "Attach the terminal to a live session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st := stateFrom(cmd)
			a, err := st.App()
			if err != nil {
				return err
			}
			name := args[0]
			aerr := a.Sessions.Attach(cmd.Context(), name)
			if errors.Is(aerr, session.ErrAttachUnsupported) {
				fmt.Fprintf(os.Stdout, "This session backend can't attach. Stream its output instead:\n  shepherd logs -f %s\n", name)
				return nil
			}
			return aerr
		},
	}
	return cmd
}

// newStopCmd terminates a session's process tree.
func newStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "stop <session>",
		Short: "Stop a running session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st := stateFrom(cmd)
			a, err := st.App()
			if err != nil {
				return err
			}
			name := args[0]
			if serr := a.Sessions.Stop(cmd.Context(), name, force); serr != nil {
				return serr
			}
			st.Out.Result(map[string]any{"session": name, "stopped": true}, func() string {
				return fmt.Sprintf("Stopped session %s.", name)
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "force-kill the process tree")
	return cmd
}

// newRmCmd removes a worktree and the session that ran in it.
func newRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <worktree>",
		Short: "Remove a worktree and its session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st := stateFrom(cmd)
			a, err := st.App()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			wt, err := a.Worktrees.Get(ctx, args[0])
			if err != nil {
				return err
			}
			// The session shares the worktree's logical name (see `new`).
			name := wt.Name
			// Best-effort: a worktree may have no associated session.
			_ = a.Sessions.Stop(ctx, name, force)
			if rerr := a.Worktrees.Remove(ctx, wt, force); rerr != nil {
				return rerr
			}
			_ = a.Sessions.Remove(ctx, name, true)

			st.Out.Result(map[string]any{"worktree": wt, "session": name, "removed": true}, func() string {
				return fmt.Sprintf("Removed worktree %s.", wt.Name)
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "force removal even if the worktree is dirty")
	return cmd
}

// endsWithNewline reports whether s ends in a newline.
func endsWithNewline(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '\n'
}
