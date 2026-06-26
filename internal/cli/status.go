package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/forge"
	"github.com/JacobRWebb/shepherd/internal/session"
)

type statusRow struct {
	Worktree domain.Worktree      `json:"worktree"`
	Session  *session.Info        `json:"session,omitempty"`
	PR       *domain.PullRequest  `json:"pr,omitempty"`
	Checks   *domain.CheckSummary `json:"checks,omitempty"`
}

func newStatusCmd() *cobra.Command {
	var showPRs, prune bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show active worktrees, sessions, and PRs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := stateFrom(cmd)
			a, err := st.App()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			if prune {
				if removed, perr := a.Worktrees.Prune(ctx, a.Cfg.Worktrees.AutoCleanup); perr == nil {
					for _, r := range removed {
						st.Log.Info().Str("worktree", r.Name).Msg("pruned")
					}
				} else {
					st.Log.Warn().Err(perr).Msg("prune failed")
				}
			}

			wts, err := a.Worktrees.List(ctx)
			if err != nil {
				return err
			}
			sessions, _ := a.Sessions.List(ctx)
			byDir := make(map[string]session.Info, len(sessions))
			for _, s := range sessions {
				byDir[filepath.Clean(s.Dir)] = s
			}

			var fg forge.Forge
			if showPRs {
				fg, _ = a.Forge()
			}

			rows := []statusRow{}
			for _, wt := range wts {
				if wt.IsMain {
					continue
				}
				row := statusRow{Worktree: wt}
				if s, ok := byDir[filepath.Clean(wt.Path)]; ok {
					si := s
					row.Session = &si
				}
				if fg != nil && wt.Branch != "" {
					if pr, perr := fg.PRForBranch(ctx, a.Repo, wt.Branch); perr == nil && pr != nil {
						row.PR = pr
						if checks, cerr := fg.ListChecks(ctx, a.Repo, pr.Number); cerr == nil {
							cs := domain.Summarize(checks)
							row.Checks = &cs
						}
					}
				}
				rows = append(rows, row)
			}

			if !st.JSON && len(rows) == 0 {
				fmt.Fprintln(os.Stdout, "No active worktrees.")
				return nil
			}

			headers := []string{"WORKTREE", "BRANCH", "SESSION", "AGENT"}
			if showPRs {
				headers = append(headers, "PR", "CHECKS")
			}
			var table [][]string
			for _, r := range rows {
				sess, agentName := "-", "-"
				if r.Session != nil {
					sess = string(r.Session.State)
					agentName = r.Session.Name
				}
				rowv := []string{r.Worktree.Name, dash(r.Worktree.Branch), sess, agentName}
				if showPRs {
					prStr, checkStr := "-", "-"
					if r.PR != nil {
						prStr = fmt.Sprintf("#%d %s", r.PR.Number, r.PR.State)
					}
					if r.Checks != nil {
						checkStr = checkSummaryStr(*r.Checks)
					}
					rowv = append(rowv, prStr, checkStr)
				}
				table = append(table, rowv)
			}

			st.Out.Table(headers, table, map[string]any{
				"worktrees":    rows,
				"generated_at": time.Now().UTC().Format(time.RFC3339),
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&showPRs, "prs", false, "look up the PR and checks for each branch")
	cmd.Flags().BoolVar(&prune, "prune", false, "prune stale worktrees (and merged branches)")
	return cmd
}

func checkSummaryStr(s domain.CheckSummary) string {
	switch {
	case len(s.Checks) == 0:
		return "-"
	case s.AnyFail:
		return fmt.Sprintf("%d failing", len(s.Failed))
	case s.Pending > 0:
		return fmt.Sprintf("%d pending", s.Pending)
	default:
		return "passing"
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
