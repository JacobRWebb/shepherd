---
name: shepherd
description: Drive the Shepherd CLI to create isolated git worktrees, launch Claude coding agents (single or crew), run the validation pipeline, and ship/babysit pull requests on GitHub or Bitbucket. Use when the user wants an isolated agent workspace or end-to-end PR management.
user-invocable: true
allowed-tools:
  - Bash(shepherd:*)
  - Read
---

# Shepherd

Shepherd is a CLI for agentic git operations. Every command accepts `--json` for
machine-readable output — **always pass `--json` when invoking Shepherd from an
agent** and parse stdout. Human-readable logs go to stderr; structured results go
to stdout, so `shepherd <cmd> --json` is safe to pipe.

## Exit codes

- `0` success
- `2` invalid input
- `3` not found
- `4` not a git repository
- `1` any other error

## Commands

- `shepherd init [--with-hooks] [--force]` — scaffold `.shepherd.yaml` and this skill.
- `shepherd new "<issue-or-task>" [--headless] [--json]` — create a worktree and
  launch an agent. Default is an interactive session; `--headless` runs to
  completion and returns a structured result.
- `shepherd crew "<description>" [-n N] [--json]` — decompose work into N parallel
  agents, one worktree each.
- `shepherd ship <branch-or-task> [--no-push] [--auto-fix] [--json]` — run the
  validation gate; if it passes, push and open a PR. `--no-push` is a dry gate.
- `shepherd babysit <pr-number> [--auto-fix] [--json]` — watch a PR's CI, auto-fix
  safe failures, and notify on anything it cannot fix.
- `shepherd status [--prs] [--prune] [--json]` — list worktrees, sessions, and PRs.

## Typical flow

1. `shepherd new "#123" --headless --json` → note the returned `worktree.path` and `session.id`.
2. Work happens in the worktree (the launched agent edits there).
3. `shepherd ship <branch> --json` → returns the PR URL.
4. `shepherd babysit <pr-number> --json` → keeps CI green.

## Notes

- Shepherd never mutates the main working tree; all work happens in worktrees
  under the configured `worktrees.root` (default `../.shepherd-worktrees`).
- The forge (GitHub via `gh`, or Bitbucket via REST) and session backend
  (tmux or native) are selected in `.shepherd.yaml`.
