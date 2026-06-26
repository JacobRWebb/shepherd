# Windows notes

Shepherd is developed and tested on Windows; these are the platform specifics.

## Executables and `.cmd` shims

External binaries are resolved with `exec.LookPath`, which is PATHEXT-aware (so
`git`/`gh`/`claude` resolve to their `.exe`). If `claude.binary` points at a
`.cmd`/`.bat` shim, Shepherd runs it through `cmd.exe /c`. A real `.exe` is invoked
directly.

## Session backend

`tmux` is not available on native Windows, so the session backend `auto` resolves to
**native**: each agent runs as a detached process (`CREATE_NEW_PROCESS_GROUP |
DETACHED_PROCESS`) with output captured to `.shepherd/logs/<name>.log`. Liveness is
derived from the OS process table plus an exit-code sentinel file, so sessions survive
`shepherd` exiting.

Process-tree termination uses `taskkill /PID <pid> /T [/F]` (claude spawns
node/git/ripgrep children that a single `Kill` would orphan).

To use the tmux backend, run Shepherd inside WSL (where tmux is present).

## Worktrees

The default worktrees root is a sibling of the repo (`../.shepherd-worktrees`) to keep
worktrees out of recursive scans (e.g. Defender) and avoid nested-repo issues. Worktree
directory names are sanitized against Windows-illegal characters and reserved device
names (CON, PRN, NUL, COM1…). Always stop a session before removing its worktree —
Windows holds file handles aggressively and `git worktree remove` will otherwise fail.

## Git hooks

`shepherd init --with-hooks` writes `#!/bin/sh` hooks; git-for-Windows runs them via
its bundled bash, so POSIX hook scripts work.

## Interactive agents

`shepherd new` (interactive) hands the terminal to claude by inheriting
stdin/stdout/stderr; the child shares the console, so Ctrl-C reaches claude directly.
