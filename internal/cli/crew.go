package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/crew"
	"github.com/JacobRWebb/shepherd/internal/domain"
)

func newCrewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crew [task-description]",
		Short: "Decompose work into parallel agents, one worktree each",
		Long:  "Decompose work into parallel agents, one worktree each.\nProvide a task description to plan automatically, or --tasks <file> with one task per line.",
		Args:  cobra.ArbitraryArgs,
		RunE:  runCrew,
	}
	cmd.Flags().IntP("agents", "n", 3, "target number of parallel agents")
	cmd.Flags().String("tasks", "", "file with one task per line (skips planning)")
	cmd.Flags().String("base", "", "base branch for the worktrees")
	cmd.Flags().String("model", "", "claude model")
	cmd.Flags().Bool("detach", false, "launch and return immediately instead of monitoring")
	cmd.Flags().Bool("keep", false, "keep worktrees after the crew finishes")
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

	if len(args) == 0 && strings.TrimSpace(tasksFile) == "" {
		return domain.InvalidInputf("provide a task description or --tasks <file>")
	}
	base, _ := cmd.Flags().GetString("base")
	model, _ := cmd.Flags().GetString("model")
	detach, _ := cmd.Flags().GetBool("detach")
	keep, _ := cmd.Flags().GetBool("keep")

	res, err := crew.Run(ctx, crew.Deps{
		Worktrees: a.Worktrees,
		Sessions:  a.Sessions,
		Agent:     ag,
		RepoRoot:  a.Paths.RepoRoot,
		Log:       a.Log,
	}, crew.Options{
		Description: strings.Join(args, " "),
		Agents:      n,
		TasksFile:   tasksFile,
		Base:        base,
		Model:       model,
		Detach:      detach,
		Keep:        keep,
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
		}
		if res.Detached {
			b.WriteString("\nLaunched detached. Use `shepherd status` to monitor.")
		} else {
			b.WriteString("\nWorktrees kept. `shepherd ship <branch>` to ship one, or remove with `git worktree remove`.")
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
