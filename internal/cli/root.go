// Package cli wires the cobra command tree, loads configuration, and renders
// results/errors via internal/output (machine output on stdout, logs on stderr).
package cli

import (
	"context"
	"os"
	"os/signal"
	"sync"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/app"
	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/logging"
	"github.com/JacobRWebb/shepherd/internal/output"
)

// cmdState carries per-invocation dependencies on the command context.
type cmdState struct {
	Cfg  config.Config
	Out  *output.Writer
	Log  *zerolog.Logger
	JSON bool
	ctx  context.Context

	appOnce sync.Once
	app     *app.App
	appErr  error
}

// App lazily builds the composition root (so init/version work outside a repo).
func (s *cmdState) App() (*app.App, error) {
	s.appOnce.Do(func() {
		s.app, s.appErr = app.New(s.ctx, s.Cfg, s.Log)
	})
	return s.app, s.appErr
}

type ctxKey struct{}

func stateFrom(cmd *cobra.Command) *cmdState {
	if st, ok := cmd.Context().Value(ctxKey{}).(*cmdState); ok {
		return st
	}
	return nil
}

var (
	flagConfig        string
	flagJSON          bool
	flagVerbose       bool
	flagNoColor       bool
	flagWorktreesRoot string
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "shepherd",
		Short:         "Shepherd — agentic git worktree, crew, and PR operations",
		Long:          "Shepherd spins up isolated git worktrees, launches Claude coding agents (single or crew),\nruns a validation pipeline, and ships/babysits pull requests on GitHub or Bitbucket.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd, args)
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(flagConfig)
			if err != nil {
				return err
			}
			if flagWorktreesRoot != "" {
				cfg.Worktrees.Root = flagWorktreesRoot
			}

			level := cfg.Logging.Level
			switch {
			case flagVerbose:
				level = "debug"
			case flagJSON:
				level = "warn" // keep stderr quiet so stdout JSON stays clean
			}
			log, err := logging.New(level, cfg.Logging.Format == "json", flagNoColor || flagJSON, cfg.Logging.File)
			if err != nil {
				return err
			}

			st := &cmdState{
				Cfg:  cfg,
				Out:  output.New(flagJSON, os.Stdout),
				Log:  log,
				JSON: flagJSON,
				ctx:  cmd.Context(),
			}
			cmd.SetContext(context.WithValue(cmd.Context(), ctxKey{}, st))
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, _ []string) error {
			maybeNotifyUpdate(cmd)
			return nil
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flagConfig, "config", "", "path to .shepherd.yaml (default: auto-discover)")
	pf.BoolVar(&flagJSON, "json", false, "machine-readable JSON output")
	pf.BoolVarP(&flagVerbose, "verbose", "v", false, "verbose (debug) logging")
	pf.BoolVar(&flagNoColor, "no-color", false, "disable colored output")
	pf.StringVar(&flagWorktreesRoot, "worktrees-root", "", "override the worktrees root directory")

	root.AddCommand(
		newInitCmd(),
		newNewCmd(),
		newResumeCmd(),
		newCrewCmd(),
		newDeliverCmd(),
		newShipCmd(),
		newBabysitCmd(),
		newStatusCmd(),
		newPsCmd(),
		newLogsCmd(),
		newAttachCmd(),
		newStopCmd(),
		newRmCmd(),
		newStatsCmd(),
		newDoctorCmd(),
		newTUICmd(),
		newUpdateCmd(),
		newVersionCmd(),
	)
	return root
}

// Execute runs the CLI and returns a process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		// flagJSON is set during flag parsing, so it reflects the user's choice.
		output.New(flagJSON, os.Stdout).Error(err)
		return output.ExitCode(err)
	}
	return 0
}
