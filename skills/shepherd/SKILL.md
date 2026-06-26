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

- `shepherd deliver "<idea>" [--json]` — **the whole loop for one idea**: design
  (grounded on the base branch) → worktree → implement + self-verify → validation
  gate with auto-fix → open a PR → babysit it (CI + review feedback) until you
  merge. `--babysit=false` stops after the PR; `--discuss` adds interactive planning.
- `shepherd init [--with-hooks] [--force]` — scaffold `.shepherd.yaml` and this skill.
- `shepherd new "<issue-or-task>" [--headless] [--json]` — create a worktree and
  launch an agent. Default is an interactive session; `--headless` runs to
  completion and returns a structured result.
- `shepherd crew "<description>" [-n N] [--ship] [--json]` — decompose work into N
  parallel agents, one worktree each. With `--ship`, each agent runs its own gate
  and opens its own PR (one idea-set in, N independent PRs out).
- `shepherd ship <branch-or-task> [--no-push] [--auto-fix] [--json]` — run the
  validation gate; if it passes, push and open a PR. `--no-push` is a dry gate.
- `shepherd babysit <pr-number> [--auto-fix] [--json]` — watch a PR's CI, auto-fix
  safe failures, **reconcile new human review comments** (judge each point,
  implement the valid ones, reply), and notify on anything it won't touch. Never merges.
- `shepherd status [--prs] [--prune] [--json]` — list worktrees, sessions, and PRs.

## Typical flow

One-shot (autonomous): `shepherd deliver "<idea>" --json` does all of the below.

Step by step (when you want control between stages):
1. `shepherd new "#123" --headless --json` → note the returned `worktree.path` and `session.id`.
2. Work happens in the worktree (the launched agent edits there).
3. `shepherd ship <branch> --json` → returns the PR URL.
4. `shepherd babysit <pr-number> --json` → keeps CI green and reconciles your feedback.

## Notes

- Shepherd never mutates the main working tree; all work happens in worktrees
  under the configured `worktrees.root` (default `../.shepherd-worktrees`).
- The forge (GitHub via `gh`, or Bitbucket via REST) and session backend
  (tmux or native) are selected in `.shepherd.yaml`.
