# Shepherd — Claude Code guide

> Agentic git-operations CLI in Go. This file orients Claude Code when working
> *on* Shepherd's own source. For driving the built tool, see [AGENTS.md](AGENTS.md).

## Overview

Shepherd creates isolated git worktrees, launches `claude` agents in them, runs a
validation pipeline, and ships/babysits PRs on GitHub or Bitbucket. It is
cross-platform and exposes `--json` on every command.

| | |
|---|---|
| Language | Go (module `github.com/JacobRWebb/shepherd`) |
| Toolchain | Go 1.25 (`go.mod` directive `go 1.25.0`) |
| CLI | `spf13/cobra` |
| Config | `knadh/koanf/v2` (+ yaml parser), layered defaults◄file◄env |
| TUI | `charmbracelet/bubbletea` v1.3.4 + `bubbles` v0.21.0 + `lipgloss` v1.1.0 (pinned) |
| Logging | `rs/zerolog` (stderr) |
| External CLIs | `git`, `claude`, `gh` (GitHub forge), `tmux` (optional backend) |

## Repository layout

```
cmd/shepherd/         entrypoint: os.Exit(cli.Execute())
internal/
  cli/                cobra commands; loads config; renders results/errors
  app/                composition root: App wires the managers for a repo
  config/             Config structs + koanf loader + embedded templates
  domain/             neutral entities + sentinel errors (imports nothing internal)
  worktree/           Manager interface + git-worktree impl (never touches main tree)
  agent/              Launcher interface + claude impl (interactive + headless)
  session/            SessionBackend interface; native + tmux backends; JSON registry
  forge/              Forge interface + factory; github/ (gh CLI), bitbucket/ (REST)
  pipeline/           validation steps + Gate (the clean push gate)
  ship/               validate → push → open PR (+ bounded auto-fix)
  babysit/            poll CI → auto-fix safe failures → reconcile review feedback → notify
  crew/               plan → fan out agents → monitor → collect (+ --ship: one PR/agent)
  deliver/            the full loop: design → worktree → implement → ship → babysit
  tui/                Bubble Tea dashboard + log viewer
  notify/             terminal / webhook / command notifiers
  output/             dual human/JSON result writer (stdout); exit-code mapping
  logging/            zerolog setup (stderr)
  gitutil/ paths/     git metadata helpers; on-disk path resolution
skills/shepherd/      the Claude skill (also embedded in the binary)
docs/                 architecture, configuration, commands, windows
```

## Architecture decisions

- **Dependency inversion at the seams.** `cli`/`app` depend on the `worktree.Manager`,
  `agent.Launcher`, `session.SessionBackend`, and `forge.Forge` *interfaces*. The
  composition root (`internal/app`) constructs the concrete impls and injects them.
- **No provider leakage.** `gh`/Bitbucket/tmux details are mapped to `internal/domain`
  types before crossing a package boundary. Forge impls live in subpackages and do
  **not** import `internal/forge` (the factory wires them) — this avoids an import cycle.
- **`config` is the only package that reads env/disk**, then the `Config` is passed
  explicitly (no globals).
- **stdout = machine output, stderr = diagnostics.** `internal/output` renders results;
  `internal/logging` writes logs to stderr so `--json` stays clean.
- **Pluggable backends.** Session backend (`auto|tmux|native`) and forge
  (`github|bitbucket`) are config-selected. The native backend works on Windows today.
- **Worktrees are shelled out** to `git worktree` (go-git's support is incomplete) and
  default to a sibling dir (`../.shepherd-worktrees`) outside the working tree.

## Common commands

```sh
make build      # -> bin/shepherd (with version ldflags)
make install    # go install with version info
make test       # go test ./...
make vet        # go vet ./...
make lint       # staticcheck ./...  (if installed)
make fmt        # gofmt -w
go build ./...  # quick compile check
GOOS=linux go build ./...   # validate the unix-tagged session files
```

## Testing

- Unit tests live next to the code (`*_test.go`), table-driven where it helps.
- Pure logic is covered directly: config validation/loading + layering, agent argv
  construction, forge JSON→domain mapping (GitHub & Bitbucket), the pipeline gate,
  updater archive extraction + version compare, the session store, and crew planning.
  Packages that shell out (gh/git/claude/tmux) are tested via their pure helpers plus
  light integration tests (e.g. a temporary `git init`).
- Run `go test ./...` (or `make test`). **CI runs the full suite on Linux and Windows for
  every push/PR**, so a regression fails the build before merge.
- When adding a feature, add or extend tests in the same package and keep
  `go test ./...` green.

## Code style

- Idiomatic Go; `gofmt`-clean; package doc comment on each package's primary file.
- Context-first signatures: `func (x *X) Do(ctx context.Context, ...) (T, error)`.
- Constructors `NewXxx(deps...)`; dependencies passed explicitly.
- Wrap errors with `%w`; compare with `errors.Is` against `domain` sentinels.
- Platform-specific code uses build tags (`native_windows.go` / `native_unix.go`).

## Important rules

- **Never** run a mutating git command against the main working tree;
  `worktree.Manager` enforces this (`RunInWorktree`/`Remove` refuse it).
- Keep the Charm TUI deps pinned (v1.3.4 / v0.21.0 / v1.1.0). The latest releases
  require newer Go and the v2 (`charm.land`) track is API-breaking.
- When adding a dependency, `go get pkg@version` right before importing it, then
  `go mod tidy` only once code imports it (mid-build tidies prune unused pins).
- Resolve external binaries via `exec.LookPath` (PATHEXT-aware); wrap `.cmd`/`.bat`
  shims through `cmd.exe /c` on Windows.
- **Commit attribution:** never add a `Co-Authored-By: Claude …` trailer and never
  set the author/committer to a Claude/Anthropic identity. Commits are the maintainer's
  alone. This applies to commits Shepherd itself makes (ship/babysit/crew) as well.

## Decision log

- _2026-06-26_ — Initial implementation. Adopted the Go 1.25 toolchain because
  current `cobra`/`koanf`/`x/sys` require ≥1.24; kept the Charm TUI libs on the v1
  track (pinned) for API stability.
- _2026-06-26_ — Bitbucket forge uses **API tokens** (Atlassian email + token over
  Basic auth), since app passwords were retired (June 9 2026).
- _2026-06-26_ — Implemented the tmux session backend alongside native (native is the
  default on Windows; `auto` prefers tmux on non-Windows when present).
- _2026-06-26_ — Stripped `Co-Authored-By: Claude` trailers from history and adopted a
  no-AI-attribution rule for all commits (human author only).
- _2026-06-26_ — Built the autonomous delivery loop. Unattended agents (crew/ship/
  babysit auto-fix, deliver) run with `bypassPermissions` (new `agent.PermissionBypass`)
  so they can run build/test and self-verify; crew streams `stream-json` for live logs.
  `crew --ship` opens one PR per agent; `babysit` reconciles human review feedback
  (judge → implement valid points → reply, never merges); `deliver "<idea>"` chains
  design → implement → gate(auto-fix) → PR → babysit. Two human touchpoints: idea, merge.
