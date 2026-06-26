package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/pipeline"
	"github.com/JacobRWebb/shepherd/internal/ship"
)

func newShipCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ship [branch-or-task]",
		Short: "Run the validation gate, push, and open a PR",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runShip,
	}
	cmd.Flags().String("base", "", "base branch for the PR")
	cmd.Flags().String("title", "", "PR title")
	cmd.Flags().String("body", "", "PR body")
	cmd.Flags().Bool("draft", false, "open the PR as a draft")
	cmd.Flags().Bool("no-push", false, "run the validation gate only; do not push or open a PR")
	cmd.Flags().Bool("auto-fix", false, "on failure, ask claude to fix and re-run the gate")
	cmd.Flags().Int("max-fix-attempts", 2, "maximum auto-fix attempts")
	cmd.Flags().StringSlice("reviewer", nil, "request reviewers")
	return cmd
}

func runShip(cmd *cobra.Command, args []string) error {
	st := stateFrom(cmd)
	a, err := st.App()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	target := ""
	if len(args) > 0 {
		target = args[0]
	}

	pcfg, err := pipeline.FromConfig(a.Cfg.Validation)
	if err != nil {
		return err
	}
	runner := pipeline.NewRunner(pcfg, a.Log)

	fg, _ := a.Forge()
	ag, _ := a.Agent()

	base, _ := cmd.Flags().GetString("base")
	title, _ := cmd.Flags().GetString("title")
	body, _ := cmd.Flags().GetString("body")
	draft, _ := cmd.Flags().GetBool("draft")
	noPush, _ := cmd.Flags().GetBool("no-push")
	autoFix, _ := cmd.Flags().GetBool("auto-fix")
	maxFix, _ := cmd.Flags().GetInt("max-fix-attempts")
	reviewers, _ := cmd.Flags().GetStringSlice("reviewer")

	res, err := ship.Run(ctx, ship.Deps{
		Worktrees: a.Worktrees,
		Forge:     fg,
		Agent:     ag,
		Runner:    runner,
		Repo:      a.Repo,
		RepoRoot:  a.Paths.RepoRoot,
		Log:       a.Log,
	}, ship.Options{
		Target:         target,
		Base:           base,
		Title:          title,
		Body:           body,
		Draft:          draft,
		AutoFix:        autoFix,
		MaxFixAttempts: maxFix,
		Reviewers:      reviewers,
		NoPush:         noPush,
	})
	if err != nil {
		return err
	}

	st.Out.Result(res, func() string {
		var b strings.Builder
		if res.GatePassed {
			b.WriteString("✓ validation passed\n")
		} else {
			b.WriteString("✗ validation failed\n")
		}
		if res.FixAttempts > 0 {
			fmt.Fprintf(&b, "  (%d auto-fix attempt(s))\n", res.FixAttempts)
		}
		switch {
		case res.Pushed:
			fmt.Fprintf(&b, "✓ pushed %s\n", res.Branch)
		case res.GatePassed:
			b.WriteString("(--no-push: gate only)\n")
		}
		if res.PR != nil {
			fmt.Fprintf(&b, "PR: %s", res.PR.URL)
		}
		return strings.TrimRight(b.String(), "\n")
	})
	return nil
}
