package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/stats"
)

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show aggregate agent usage statistics (cost, turns, tokens)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := stateFrom(cmd)
			a, err := st.App()
			if err != nil {
				return err
			}

			report, err := stats.Collect(a.Store)
			if err != nil {
				return err
			}

			headers := []string{"MODEL", "RUNS", "TURNS", "INPUT", "OUTPUT", "COST"}
			table := make([][]string, 0, len(report.ByModel)+1)
			for _, m := range report.ByModel {
				table = append(table, []string{
					m.Model,
					fmt.Sprintf("%d", m.Runs),
					fmt.Sprintf("%d", m.Turns),
					fmt.Sprintf("%d", m.InputTokens),
					fmt.Sprintf("%d", m.OutputTokens),
					fmt.Sprintf("$%.4f", m.CostUSD),
				})
			}
			t := report.Totals
			table = append(table, []string{
				"TOTAL",
				fmt.Sprintf("%d", t.Runs),
				fmt.Sprintf("%d", t.Turns),
				fmt.Sprintf("%d", t.InputTokens),
				fmt.Sprintf("%d", t.OutputTokens),
				fmt.Sprintf("$%.4f", t.CostUSD),
			})

			st.Out.Table(headers, table, report)
			return nil
		},
	}
	return cmd
}
