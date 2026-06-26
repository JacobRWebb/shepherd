# Architecture

Shepherd follows an N-tier layout with dependency inversion at the seams.

## Layers

```
cmd/shepherd ──► internal/cli ──► internal/app ──► { worktree, agent, session, forge,
                      │                              pipeline, ship, babysit, crew, notify }
                      │                                        │
                      └──► internal/output            all map to ──► internal/domain
internal/logging is used by cli/app only. internal/domain imports nothing internal.
```

- **`cmd/shepherd`** — `os.Exit(cli.Execute())`, nothing else.
- **`internal/cli`** — cobra commands. Loads config in `PersistentPreRunE`, builds the
  logger + output writer, and lazily constructs `app.App`. Renders results via
  `internal/output` and maps errors to exit codes.
- **`internal/app`** — the composition root. `New(ctx, cfg, log)` resolves the repo
  root, paths, default branch, then constructs the worktree manager, session backend,
  agent launcher, and forge, injecting them behind interfaces. The agent and forge are
  built best-effort (errors deferred to `App.Agent()`/`App.Forge()`) so `init`/`status`
  work without claude/a forge.
- **`internal/domain`** — neutral entities (`Task`, `Worktree`, `PullRequest`, `Check`,
  `Comment`, `Issue`, `Repo`, `OpenPROpts`) and sentinel errors. Imports nothing internal.

## Seams (interfaces)

| Interface | Package | Implementations |
|---|---|---|
| `worktree.Manager` | `internal/worktree` | `Git` (shells out to `git worktree`) |
| `agent.Launcher` | `internal/agent` | `Claude` (shells out to the `claude` CLI) |
| `session.SessionBackend` | `internal/session` | `nativeBackend`, `tmuxBackend` |
| `forge.Forge` | `internal/forge` | `github.Client` (gh CLI), `bitbucket.Client` (REST) |
| `pipeline.Runner` | `internal/pipeline` | concrete `Runner` |

**Avoiding an import cycle:** the forge impls (`forge/github`, `forge/bitbucket`)
depend only on `internal/domain`. `forge.Select` (the factory) imports the impls and
returns them as `forge.Forge` — that's where the compiler verifies they satisfy the
interface. The interface itself uses `domain.Repo`/`domain.OpenPROpts` so impls never
need to import `forge`.

## Data flow examples

- **`new`**: cli → `worktree.Create` → `agent.Interactive`/`Headless` (or, for
  `--detach`, `session.Start`).
- **`ship`**: cli builds `pipeline.Runner` → `ship.Run` → `Runner.Gate` →
  `git push` → `forge.OpenPR`.
- **`crew`**: cli → `crew.Run` → `agent.Headless` (planner) → `worktree.Create` ×N →
  `session.Start` ×N → poll `session.List` → collect.
- **`babysit`**: cli → `babysit.Run` → poll `forge.GetPR`/`ListChecks` →
  `agent.Headless` (fix) → `git push` → `forge.PostComment` → `notify`.

## Runtime state

- Worktrees: `worktrees.root` (default `../.shepherd-worktrees`, outside the repo).
- Session registry: `<repo>/.shepherd/sessions.json` (file-locked via `gofrs/flock`).
- Native session logs: `<repo>/.shepherd/logs/<name>.log` (+ `.exit` sentinel).
