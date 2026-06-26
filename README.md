# Shepherd 🐑

Shepherd is a Go CLI for **agentic git operations**. It spins up isolated git
worktrees, launches Claude coding agents in them (one at a time or a parallel
*crew*), runs a configurable validation pipeline, and ships/babysits pull
requests on **GitHub or Bitbucket**.

It is cross-platform (Windows, macOS, Linux) and agent-friendly: every command
supports `--json`.

## Why

- **Isolation by default** — all agent work happens in disposable worktrees, never
  in your main working tree.
- **One tool, end to end** — create a workspace, run an agent, validate, open a PR,
  and keep CI green.
- **Pluggable** — choose your session backend (native processes or tmux) and your
  forge (GitHub via `gh`, or Bitbucket via REST) in config.

## Install

**One-line install (prebuilt binary):**

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/JacobRWebb/shepherd/main/install.sh | sh

# Windows (PowerShell)
irm https://raw.githubusercontent.com/JacobRWebb/shepherd/main/install.ps1 | iex
```

**With Go:**

```sh
go install github.com/JacobRWebb/shepherd/cmd/shepherd@latest
```

**From source:**

```sh
git clone https://github.com/JacobRWebb/shepherd && cd shepherd
make install      # builds with version info into $GOBIN
```

Or download a binary for your platform from the [latest release](https://github.com/JacobRWebb/shepherd/releases/latest).

Requirements on `PATH`: `git`, the `claude` CLI, and — for the GitHub forge — `gh`.
tmux is optional (used by the tmux session backend on Linux/macOS/WSL).

## Quick start

```sh
shepherd init                       # scaffold .shepherd.yaml + the Claude skill
shepherd new "fix the login timeout" # worktree + interactive agent
shepherd new "#123" --headless      # worktree + autonomous agent, from an issue
shepherd ship <branch> --no-push    # run the validation gate (dry run)
shepherd ship <branch>              # validate, push, open a PR
shepherd crew "refactor billing" -n 3   # 3 parallel agents, one worktree each
shepherd babysit 42                 # watch PR #42's CI, auto-fix safe failures
shepherd status                     # list worktrees, sessions, PRs
shepherd                            # interactive dashboard (TUI)
```

## Commands

| Command | Purpose |
|---|---|
| `init` | Scaffold `.shepherd.yaml`, the skill, and optional git hooks |
| `new <issue-or-task>` | Create a worktree and launch an agent (interactive or `--headless`) |
| `crew <description>` | Decompose work into N parallel agents, one worktree each |
| `ship <branch-or-task>` | Run the validation gate, push, open a PR (`--auto-fix` to self-heal) |
| `babysit <pr-number>` | Watch CI, auto-fix safe failures, notify on the rest |
| `status` | Show worktrees, sessions, and PRs (`--prs`, `--prune`) |
| `update` | Self-update to the latest release (`--check` to only check) |
| `tui` (or no args) | Interactive dashboard |

Run `shepherd <command> --help` for flags. See [docs/commands.md](docs/commands.md)
for the full reference and the `--json` output schemas.

## Configuration

Configuration lives in `.shepherd.yaml` (auto-discovered by walking up from the
working directory). Env overrides use `SHEPHERD_` with `__` for nesting, e.g.
`SHEPHERD_CLAUDE__MODEL=opus`. See [docs/configuration.md](docs/configuration.md).

## Architecture

```
cmd/shepherd        → tiny entrypoint
internal/cli        → cobra commands, --json rendering
internal/app        → composition root (wires the managers)
internal/worktree   → git worktree management
internal/agent      → claude CLI invocation (interactive + headless)
internal/session    → pluggable backends: native processes, tmux
internal/forge      → pluggable providers: GitHub (gh), Bitbucket (REST)
internal/pipeline   → validation steps + the clean push gate
internal/ship       → validate → push → open PR
internal/babysit    → poll CI, auto-fix, notify
internal/crew       → plan → fan out agents → monitor
internal/tui        → Bubble Tea dashboard
internal/{config,domain,output,logging,notify,gitutil,paths}
```

See [docs/architecture.md](docs/architecture.md) for the layering and seams, and
[AGENTS.md](AGENTS.md) for how an agent should drive Shepherd.

## License

MIT (see LICENSE).
