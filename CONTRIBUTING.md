# Contributing to Shepherd

Thanks for your interest in contributing! This guide covers the basics of
building, testing, and running Shepherd locally. For deeper architecture notes,
see [CLAUDE.md](CLAUDE.md) and the [docs/](docs/) directory.

## Prerequisites

- **Go 1.25+** (the `go.mod` directive is `go 1.25.0`).
- **git** on your `PATH`.
- Optional, only needed at runtime (not to build/test):
  - **`claude`** CLI — to launch agents.
  - **`gh`** — for the GitHub forge.
  - **tmux** — for the tmux session backend (Linux/macOS/WSL).

Most tasks are driven through the [`Makefile`](Makefile).

## Build

```sh
make build      # -> bin/shepherd (with version ldflags)
make install    # go install into $GOBIN (with version info)
go build ./...  # quick compile check
```

To sanity-check the unix build-tagged files from any host:

```sh
make cross      # builds for linux/amd64 and darwin/arm64
```

## Test

```sh
make test       # go test ./...
make vet        # go vet ./...
make lint       # staticcheck ./...  (if installed)
make fmt        # gofmt -w .
```

CI runs the full suite on **Linux and Windows** for every push and PR, so keep
`go test ./...` green. Add or extend tests in the same package as your change —
unit tests live next to the code (`*_test.go`), table-driven where it helps.

## Run

```sh
make run ARGS="status"          # build, then run with arguments
make run ARGS="new 'fix login'" # spin up a worktree + agent

# or run directly without installing:
go run ./cmd/shepherd status
```

Every command supports `--json` for machine-readable output. For driving the
built tool, see [AGENTS.md](AGENTS.md) and `shepherd --help`.

## Pull requests

1. Branch off `main`.
2. Make your change with tests; keep `make test`, `make vet`, and `make fmt`
   clean.
3. Follow the existing code style — idiomatic, `gofmt`-clean Go with
   context-first signatures and errors wrapped via `%w`.
4. Open a PR against `main`. CI must pass on both Linux and Windows.
