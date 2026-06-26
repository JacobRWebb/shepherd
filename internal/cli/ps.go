package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newPsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ps",
		Aliases: []string{"sessions"},
		Short:   "List all agent sessions from the registry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := stateFrom(cmd)
			a, err := st.App()
			if err != nil {
				return err
			}
			sessions, err := a.Sessions.List(cmd.Context())
			if err != nil {
				return err
			}
			if !st.JSON && len(sessions) == 0 {
				fmt.Println("No sessions.")
				return nil
			}
			headers := []string{"NAME", "STATE", "PID", "BACKEND", "STARTED", "CREW", "TASK"}
			rows := make([][]string, 0, len(sessions))
			for _, s := range sessions {
				rows = append(rows, []string{
					s.Name,
					string(s.State),
					psPID(s.PID),
					string(s.Backend),
					psAge(s.StartedAt),
					dash(s.Labels["crew_id"]),
					dash(psClip(s.Labels["task"], 40)),
				})
			}
			st.Out.Table(headers, rows, sessions)
			return nil
		},
	}
}

func psPID(p int) string {
	if p <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", p)
}

func psAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return time.Since(t).Round(time.Second).String()
}

func psClip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
