package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/babysit"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/notify"
)

func newBabysitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "babysit <pr-number>",
		Short: "Watch a PR's CI, auto-fix safe failures, and notify",
		Args:  cobra.ExactArgs(1),
		RunE:  runBabysit,
	}
	cmd.Flags().Duration("interval", 30*time.Second, "base poll interval")
	cmd.Flags().Int("max-iterations", 40, "hard cap on poll cycles")
	cmd.Flags().Int("max-fix-attempts", 3, "cap on auto-fix actions")
	cmd.Flags().Bool("auto-fix", true, "auto-fix safe failures (false = watch + notify only)")
	return cmd
}

func runBabysit(cmd *cobra.Command, args []string) error {
	st := stateFrom(cmd)
	a, err := st.App()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	prNum, err := strconv.Atoi(strings.TrimPrefix(args[0], "#"))
	if err != nil {
		return domain.InvalidInputf("pr-number must be an integer: %q", args[0])
	}

	fg, ferr := a.Forge()
	if ferr != nil {
		return ferr
	}
	ag, _ := a.Agent()
	notifier := notify.New(a.Cfg.Notifications, a.Log)

	interval, _ := cmd.Flags().GetDuration("interval")
	maxIter, _ := cmd.Flags().GetInt("max-iterations")
	maxFix, _ := cmd.Flags().GetInt("max-fix-attempts")
	autoFix, _ := cmd.Flags().GetBool("auto-fix")

	if err := babysit.Run(ctx, babysit.Deps{
		Forge:     fg,
		Agent:     ag,
		Worktrees: a.Worktrees,
		Notifier:  notifier,
		Repo:      a.Repo,
		Log:       a.Log,
	}, babysit.Options{
		PR:             prNum,
		Interval:       interval,
		MaxIterations:  maxIter,
		MaxFixAttempts: maxFix,
		AutoFix:        autoFix,
	}); err != nil {
		return err
	}

	st.Out.Result(map[string]any{"pr": prNum, "done": true}, func() string {
		return fmt.Sprintf("babysit for PR #%d finished", prNum)
	})
	return nil
}
