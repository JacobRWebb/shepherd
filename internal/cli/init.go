package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/gitutil"
	"github.com/JacobRWebb/shepherd/internal/sysproc"
)

func newInitCmd() *cobra.Command {
	var force, withHooks, noSkill, bare, gitInit bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold .shepherd.yaml, the Claude skill, and (optionally) git hooks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := stateFrom(cmd)
			res, err := runInit(cmd.Context(), initOpts{
				force: force, withHooks: withHooks, noSkill: noSkill, bare: bare, gitInit: gitInit,
			})
			if err != nil {
				return err
			}
			st.Out.Result(res, func() string {
				var b strings.Builder
				for _, c := range res.Created {
					fmt.Fprintf(&b, "  created  %s\n", c)
				}
				for _, s := range res.Skipped {
					fmt.Fprintf(&b, "  skipped  %s (exists; use --force)\n", s)
				}
				b.WriteString("Shepherd initialized.")
				return b.String()
			})
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&force, "force", false, "overwrite existing files")
	f.BoolVar(&withHooks, "with-hooks", false, "install git hooks")
	f.BoolVar(&noSkill, "no-skill", false, "do not write skills/shepherd/SKILL.md")
	f.BoolVar(&bare, "bare", false, "write only .shepherd.yaml (no skill, no gitignore)")
	f.BoolVar(&gitInit, "git-init", false, "run `git init` if not already a repository")
	return cmd
}

type initOpts struct {
	force, withHooks, noSkill, bare, gitInit bool
}

type initResult struct {
	Root    string   `json:"root"`
	Created []string `json:"created"`
	Skipped []string `json:"skipped"`
	Hooks   bool     `json:"hooks"`
}

func runInit(ctx context.Context, o initOpts) (initResult, error) {
	res := initResult{}

	root, err := gitutil.RepoRoot(ctx, "")
	if err != nil {
		cwd, _ := os.Getwd()
		if o.gitInit {
			gitInitCmd := exec.CommandContext(ctx, "git", "init", "-b", "main")
			sysproc.Hide(gitInitCmd)
			if gerr := gitInitCmd.Run(); gerr != nil {
				return res, fmt.Errorf("git init: %w", gerr)
			}
		}
		root = cwd
	}
	res.Root = root

	// .shepherd.yaml
	cfgPath := filepath.Join(root, ".shepherd.yaml")
	if created, err := writeIfAbsent(cfgPath, config.DefaultConfigYAML, o.force); err != nil {
		return res, err
	} else if created {
		res.Created = append(res.Created, rel(root, cfgPath))
	} else {
		res.Skipped = append(res.Skipped, rel(root, cfgPath))
	}

	if !o.bare && !o.noSkill {
		skillPath := filepath.Join(root, "skills", "shepherd", "SKILL.md")
		if created, err := writeIfAbsent(skillPath, config.SkillTemplate, o.force); err != nil {
			return res, err
		} else if created {
			res.Created = append(res.Created, rel(root, skillPath))
		} else {
			res.Skipped = append(res.Skipped, rel(root, skillPath))
		}
	}

	if !o.bare {
		if err := ensureGitignore(root); err != nil {
			return res, err
		}
	}

	if o.withHooks {
		if err := installHooks(root, o.force); err != nil {
			return res, err
		}
		res.Hooks = true
		res.Created = append(res.Created, ".git/hooks/post-merge")
	}

	return res, nil
}

func writeIfAbsent(path, content string, force bool) (bool, error) {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func ensureGitignore(root string) error {
	path := filepath.Join(root, ".gitignore")
	const marker = ".shepherd/"
	if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), marker) {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString("\n# Shepherd runtime state\n.shepherd/\n.shepherd-worktrees/\n")
	return err
}

func installHooks(root string, force bool) error {
	hookDir := filepath.Join(root, ".git", "hooks")
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return fmt.Errorf("not a git repository; cannot install hooks")
	}
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return err
	}
	// post-merge: prune worktrees whose branches merged after a pull.
	hook := "#!/bin/sh\n# Installed by `shepherd init --with-hooks`.\nshepherd status --prune --json >/dev/null 2>&1 || true\n"
	path := filepath.Join(hookDir, "post-merge")
	if !force {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	return os.WriteFile(path, []byte(hook), 0o755)
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}
