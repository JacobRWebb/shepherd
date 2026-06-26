package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/agent"
)

func newResumeCmd() *cobra.Command {
	var headless, fork bool
	var sessionID, model, permMode, prompt string
	cmd := &cobra.Command{
		Use:   "resume <worktree-or-branch> [extra prompt...]",
		Short: "Resume the most recent agent conversation in a worktree",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st := stateFrom(cmd)
			a, err := st.App()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			arg := args[0]
			wt, err := a.Worktrees.Get(ctx, arg)
			if err != nil {
				return err
			}

			// Extra positional args become the prompt unless --prompt overrides them.
			promptText := prompt
			if promptText == "" && len(args) > 1 {
				promptText = strings.Join(args[1:], " ")
			}

			spec := agent.Spec{
				Prompt:         promptText,
				Model:          model,
				PermissionMode: permMode,
				ForkSession:    fork,
			}
			// --session pins a specific conversation; otherwise --continue resumes
			// the most recent one in the worktree.
			if sessionID != "" {
				spec.ResumeSessionID = sessionID
			} else {
				spec.ContinueSession = true
			}

			launcher, lerr := a.Agent()
			if lerr != nil {
				return lerr
			}

			if !headless {
				// Interactive: hand the terminal to claude (claude --continue).
				fmt.Fprintf(os.Stdout, "Resuming claude in worktree %s on branch %s\n  %s\n", wt.Name, wt.Branch, wt.Path)
				code, ierr := launcher.Interactive(context.Background(), wt, agent.InteractiveSpec{Spec: spec})
				if ierr != nil {
					return ierr
				}
				if code != 0 {
					return fmt.Errorf("claude exited with code %d", code)
				}
				return nil
			}

			// Headless.
			hspec := agent.HeadlessSpec{Spec: spec, OutputFormat: a.Cfg.Claude.Headless.OutputFormat}
			result, herr := launcher.Headless(ctx, wt, hspec)
			if herr != nil {
				return herr
			}
			res := map[string]any{"worktree": wt, "result": result}
			st.Out.Result(res, func() string {
				status := "ok"
				if result.IsError || result.ExitCode != 0 {
					status = "error"
				}
				return fmt.Sprintf("Worktree %s (%s)\nAgent finished [%s]:\n\n%s", wt.Name, wt.Branch, status, result.Text)
			})
			if result.IsError || result.ExitCode != 0 {
				return fmt.Errorf("agent run failed (exit %d)", result.ExitCode)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&headless, "headless", false, "run the agent headlessly to completion")
	f.StringVar(&sessionID, "session", "", "resume a specific claude session id (instead of the most recent)")
	f.BoolVar(&fork, "fork", false, "fork the resumed session into a new session")
	f.StringVar(&model, "model", "", "claude model (alias or full name)")
	f.StringVar(&permMode, "permission-mode", "", "claude permission mode")
	f.StringVar(&prompt, "prompt", "", "extra prompt to send when resuming")
	return cmd
}
