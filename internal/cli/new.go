package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/agent"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/session"
)

func newNewCmd() *cobra.Command {
	var headless, detach, skipPerms bool
	var base, model, permMode, effort, prompt, promptFile string
	cmd := &cobra.Command{
		Use:   "new <issue-or-task>",
		Short: "Create an isolated worktree and launch an agent",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st := stateFrom(cmd)
			a, err := st.App()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			task := domain.NewTask(strings.Join(args, " "))
			if task.Source == domain.TaskSourceIssue {
				if fg, ferr := a.Forge(); ferr == nil {
					if iss, ierr := fg.GetIssue(ctx, a.Repo, task.IssueID); ierr == nil {
						if iss.Title != "" {
							task.Title = iss.Title
						}
						if iss.Body != "" {
							task.Body = iss.Body
						}
					}
				}
			}

			wt, err := a.Worktrees.Create(ctx, task, base)
			if err != nil {
				return err
			}

			promptText := prompt
			if promptFile != "" {
				b, rerr := os.ReadFile(promptFile)
				if rerr != nil {
					return rerr
				}
				promptText = string(b)
			}
			if promptText == "" {
				promptText = composePrompt(task)
			}

			sessionID := uuid.NewString()
			spec := agent.Spec{
				Prompt:          promptText,
				Model:           model,
				PermissionMode:  permMode,
				SkipPermissions: skipPerms,
				Effort:          effort,
				SessionID:       sessionID,
			}

			launcher, lerr := a.Agent()
			if lerr != nil {
				return lerr
			}

			res := map[string]any{"worktree": wt, "task": task}
			runHeadless := headless || detach || st.JSON

			if !runHeadless {
				// Interactive: hand the terminal to claude.
				if a.Sessions.Kind() == session.BackendTmux {
					name := wt.Name
					info, serr := a.Sessions.Start(ctx, session.Spec{
						Name: name, Dir: wt.Path, Program: launcher.Binary(),
						Args:   launcher.InteractiveArgs(agent.InteractiveSpec{Spec: spec}),
						Labels: labels(task, wt, sessionID),
					})
					if serr != nil {
						return serr
					}
					_ = info
					fmt.Fprintf(os.Stdout, "Created worktree %s (%s); attaching tmux session %q...\n", wt.Name, wt.Branch, name)
					return a.Sessions.Attach(context.Background(), name)
				}
				fmt.Fprintf(os.Stdout, "Created worktree %s on branch %s\n  %s\nLaunching claude...\n", wt.Name, wt.Branch, wt.Path)
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
			if detach {
				name := wt.Name
				info, serr := a.Sessions.Start(ctx, session.Spec{
					Name: name, Dir: wt.Path, Program: launcher.Binary(),
					Args:   launcher.HeadlessArgs(hspec),
					Labels: labels(task, wt, sessionID),
				})
				if serr != nil {
					return serr
				}
				res["session"] = info
				st.Out.Result(res, func() string {
					return fmt.Sprintf("Created worktree %s; launched detached agent (session %s).", wt.Name, name)
				})
				return nil
			}

			result, herr := launcher.Headless(ctx, wt, hspec)
			if herr != nil {
				return herr
			}
			res["result"] = result
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
	f.BoolVar(&detach, "detach", false, "launch a detached background agent and return immediately")
	f.BoolVar(&skipPerms, "skip-permissions", false, "pass --dangerously-skip-permissions to claude")
	f.StringVar(&base, "base", "", "base branch for the new worktree")
	f.StringVar(&model, "model", "", "claude model (alias or full name)")
	f.StringVar(&permMode, "permission-mode", "", "claude permission mode")
	f.StringVar(&effort, "effort", "", "claude effort level")
	f.StringVar(&prompt, "prompt", "", "explicit prompt (overrides the task-derived prompt)")
	f.StringVar(&promptFile, "prompt-file", "", "read the prompt from a file")
	return cmd
}

func composePrompt(task domain.Task) string {
	var b strings.Builder
	if task.Title != "" {
		b.WriteString("Task: " + task.Title + "\n")
	}
	if task.Body != "" {
		b.WriteString("\n" + task.Body + "\n")
	}
	if b.Len() == 0 {
		b.WriteString(task.Raw)
	}
	b.WriteString("\nWork only in this worktree. When you're done, summarize what you changed.")
	return b.String()
}

func labels(task domain.Task, wt domain.Worktree, sessionID string) map[string]string {
	return map[string]string{
		"task":              task.Title,
		"branch":            wt.Branch,
		"claude_session_id": sessionID,
	}
}
