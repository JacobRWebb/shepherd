package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/deliver"
	"github.com/JacobRWebb/shepherd/internal/notify"
	"github.com/JacobRWebb/shepherd/internal/pipeline"
)

func newDeliverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deliver <idea>",
		Short: "Autonomously design, implement, test, open a PR, and babysit it to merge",
		Long: "deliver runs the whole loop for one idea: an agent studies the repo and proposes an\n" +
			"approach, Shepherd opens a worktree, an agent implements and self-verifies, the\n" +
			"validation gate runs (with bounded auto-fix), a PR is opened, and babysit watches CI\n" +
			"and reconciles your review feedback until you merge.",
		Args: cobra.MinimumNArgs(1),
		RunE: runDeliver,
	}
	cmd.Flags().String("base", "", "base branch to fork from (default: repo default branch)")
	cmd.Flags().String("model", "", "claude model")
	cmd.Flags().Bool("design", true, "run the design pass that grounds the plan in the codebase")
	cmd.Flags().Bool("discuss", false, "open an interactive planning session before implementing")
	cmd.Flags().Bool("draft", false, "open the PR as a draft")
	cmd.Flags().StringSlice("reviewer", nil, "request reviewers on the PR")
	cmd.Flags().Int("max-fix-attempts", 3, "max auto-fix attempts for the gate and CI/feedback")
	cmd.Flags().Bool("babysit", true, "after opening the PR, watch CI and reconcile feedback until merge")
	cmd.Flags().Duration("interval", 30*time.Second, "babysit base poll interval")
	cmd.Flags().Int("max-iterations", 80, "babysit hard cap on poll cycles")
	return cmd
}

func runDeliver(cmd *cobra.Command, args []string) error {
	st := stateFrom(cmd)
	a, err := st.App()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	ag, aerr := a.Agent()
	if aerr != nil {
		return aerr
	}
	fg, ferr := a.Forge()
	if ferr != nil {
		return ferr
	}
	pcfg, perr := pipeline.FromConfig(a.Cfg.Validation)
	if perr != nil {
		return perr
	}

	base, _ := cmd.Flags().GetString("base")
	model, _ := cmd.Flags().GetString("model")
	design, _ := cmd.Flags().GetBool("design")
	discuss, _ := cmd.Flags().GetBool("discuss")
	draft, _ := cmd.Flags().GetBool("draft")
	reviewers, _ := cmd.Flags().GetStringSlice("reviewer")
	maxFix, _ := cmd.Flags().GetInt("max-fix-attempts")
	doBabysit, _ := cmd.Flags().GetBool("babysit")
	interval, _ := cmd.Flags().GetDuration("interval")
	maxIter, _ := cmd.Flags().GetInt("max-iterations")

	if discuss && st.JSON {
		discuss = false // interactive handoff is meaningless under --json
	}

	res, err := deliver.Run(ctx, deliver.Deps{
		Worktrees: a.Worktrees,
		Agent:     ag,
		Forge:     fg,
		Runner:    pipeline.NewRunner(pcfg, a.Log),
		Notifier:  notify.New(a.Cfg.Notifications, a.Log),
		Repo:      a.Repo,
		RepoRoot:  a.Paths.RepoRoot,
		Log:       a.Log,
	}, deliver.Options{
		Idea:                 strings.Join(args, " "),
		Base:                 base,
		Model:                model,
		Design:               design,
		Discuss:              discuss,
		Draft:                draft,
		Reviewers:            reviewers,
		MaxFixAttempts:       maxFix,
		Babysit:              doBabysit,
		BabysitInterval:      interval,
		BabysitMaxIterations: maxIter,
	})
	if err != nil {
		return err
	}

	st.Out.Result(res, func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "Delivered: %s\n", deliverHeadline(res.Idea))
		fmt.Fprintf(&b, "  branch: %s\n", res.Branch)
		if res.Ship.GatePassed {
			b.WriteString("  gate:   ✓ passed")
			if res.Ship.FixAttempts > 0 {
				fmt.Fprintf(&b, " (%d auto-fix attempt(s))", res.Ship.FixAttempts)
			}
			b.WriteString("\n")
		} else {
			b.WriteString("  gate:   ✗ failed\n")
		}
		if res.PRURL != "" {
			fmt.Fprintf(&b, "  PR:     %s\n", res.PRURL)
		}
		if res.Babysat {
			b.WriteString("  babysit finished (PR resolved, merged/closed, or watch budget reached)")
		} else if res.PRURL != "" {
			fmt.Fprintf(&b, "  next:   shepherd babysit %s", prNumberFromURL(res.PRURL))
		}
		return strings.TrimRight(b.String(), "\n")
	})
	return nil
}

func deliverHeadline(idea string) string {
	idea = strings.TrimSpace(idea)
	if i := strings.IndexByte(idea, '\n'); i >= 0 {
		idea = strings.TrimSpace(idea[:i])
	}
	if len(idea) > 72 {
		idea = strings.TrimSpace(idea[:72]) + "…"
	}
	return idea
}

func prNumberFromURL(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 && i+1 < len(url) {
		return "#" + url[i+1:]
	}
	return url
}
