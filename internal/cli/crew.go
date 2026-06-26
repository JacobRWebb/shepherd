package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/crew"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/pipeline"
)

func newCrewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crew <task-description>",
		Short: "Decompose work into parallel agents, one worktree each",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runCrew,
	}
	cmd.Flags().IntP("agents", "n", 3, "target number of parallel agents")
	cmd.Flags().String("tasks", "", "file with one task per line (skips planning)")
	cmd.Flags().String("base", "", "base branch for the worktrees")
	cmd.Flags().String("model", "", "claude model")
	cmd.Flags().Bool("detach", false, "launch and return immediately instead of monitoring")
	cmd.Flags().Bool("keep", false, "keep worktrees after the crew finishes")
	cmd.Flags().Bool("ship", false, "after each agent finishes, run its gate and open its own PR")
	cmd.Flags().Bool("draft", false, "open shipped PRs as drafts")
	cmd.Flags().Bool("auto-fix", true, "when shipping, let an agent fix a failing gate before pushing")
	cmd.Flags().Int("max-fix-attempts", 2, "max auto-fix attempts per agent when shipping")
	cmd.Flags().StringSlice("reviewer", nil, "request reviewers on shipped PRs")
	return cmd
}

func runCrew(cmd *cobra.Command, args []string) error {
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

	n, _ := cmd.Flags().GetInt("agents")
	tasksFile, _ := cmd.Flags().GetString("tasks")
	base, _ := cmd.Flags().GetString("base")
	model, _ := cmd.Flags().GetString("model")
	detach, _ := cmd.Flags().GetBool("detach")
	keep, _ := cmd.Flags().GetBool("keep")
	doShip, _ := cmd.Flags().GetBool("ship")
	draft, _ := cmd.Flags().GetBool("draft")
	autoFix, _ := cmd.Flags().GetBool("auto-fix")
	maxFix, _ := cmd.Flags().GetInt("max-fix-attempts")
	reviewers, _ := cmd.Flags().GetStringSlice("reviewer")

	deps := crew.Deps{
		Worktrees: a.Worktrees,
		Sessions:  a.Sessions,
		Agent:     ag,
		Repo:      a.Repo,
		RepoRoot:  a.Paths.RepoRoot,
		Log:       a.Log,
	}

	if doShip {
		if detach {
			return domain.InvalidInputf("--ship cannot be combined with --detach (shipping needs the agents to finish)")
		}
		fg, ferr := a.Forge()
		if ferr != nil {
			return ferr
		}
		pcfg, perr := pipeline.FromConfig(a.Cfg.Validation)
		if perr != nil {
			return perr
		}
		deps.Forge = fg
		deps.Runner = pipeline.NewRunner(pcfg, a.Log)
	}

	res, err := crew.Run(ctx, deps, crew.Options{
		Description:    strings.Join(args, " "),
		Agents:         n,
		TasksFile:      tasksFile,
		Base:           base,
		Model:          model,
		Detach:         detach,
		Keep:           keep,
		Ship:           doShip,
		Draft:          draft,
		AutoFix:        autoFix,
		MaxFixAttempts: maxFix,
		Reviewers:      reviewers,
	})
	if err != nil {
		return err
	}

	st.Out.Result(res, func() string {
		var b strings.Builder
		fmt.Fprintf(&b, "Crew %s — %d agent(s)\n", res.CrewID, len(res.Agents))
		for _, ag := range res.Agents {
			fmt.Fprintf(&b, "\n[%d] %s  (%s, branch %s)\n", ag.Index, ag.Task.Title, ag.State, ag.Branch)
			if ag.Summary != "" {
				fmt.Fprintf(&b, "    %s\n", ag.Summary)
			}
			if ag.DiffStat != "" {
				fmt.Fprintf(&b, "    changes:\n%s\n", indent(ag.DiffStat, "      "))
			}
			switch {
			case ag.PRURL != "":
				fmt.Fprintf(&b, "    PR: %s\n", ag.PRURL)
			case ag.ShipError != "":
				fmt.Fprintf(&b, "    ship failed: %s\n", ag.ShipError)
			}
		}
		switch {
		case res.Detached:
			b.WriteString("\nLaunched detached. Use `shepherd status` to monitor.")
		case doShip:
			b.WriteString("\nShipped each agent's work as its own PR. `shepherd babysit <pr>` to watch one to merge.")
		default:
			b.WriteString("\nWorktrees kept. `shepherd ship <branch>` to ship one.")
		}
		return strings.TrimRight(b.String(), "\n")
	})
	return nil
}

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
