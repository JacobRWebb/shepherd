# Command reference

Every command accepts the global flags `--json`, `--verbose/-v`, `--config <path>`,
`--no-color`, and `--worktrees-root <dir>`. With `--json`, stdout carries
`{"ok":true,"data":...}` or `{"ok":false,"error":{"code","message"}}`.

## `init`
Scaffold config and the skill. Flags: `--force`, `--with-hooks`, `--no-skill`,
`--bare`, `--git-init`.

`data`: `{ root, created[], skipped[], hooks }`.

## `new <issue-or-task>`
Create a worktree and launch an agent. Flags: `--headless`, `--detach`,
`--skip-permissions`, `--base`, `--model`, `--permission-mode`, `--effort`,
`--prompt`, `--prompt-file`.

- Default: interactive (hands the terminal to claude). `--json` implies `--headless`.
- `--headless`: runs `claude -p` to completion and returns the result.
- `--detach`: launches a background session and returns immediately.

`data`: `{ worktree:{name,path,branch,head}, task:{raw,title,source,issue_id?},
session?:{name,state,...}, result?:{text,is_error,exit_code,cost_usd,num_turns} }`.

## `crew <task-description>`
Decompose work into parallel agents. Flags: `-n/--agents`, `--tasks <file>`,
`--base`, `--model`, `--detach`, `--keep`.

`data`: `{ crew_id, tasks[], agents[]:{index,task,name,branch,path,state,exit_code?,
summary?,diffstat?}, detached }`.

## `ship [branch-or-task]`
Run the gate, push, open a PR. Flags: `--base`, `--title`, `--body`, `--draft`,
`--no-push`, `--auto-fix`, `--max-fix-attempts`, `--reviewer`.

`data`: `{ branch, dir, gate_passed, pipeline:{steps[],passed,duration},
fix_attempts, pushed, pr?:{number,url,state,...} }`.

A failed gate (without `--auto-fix`) exits 2 with the failure digest in the error.

## `babysit <pr-number>`
Watch CI and auto-fix safe failures. Flags: `--interval`, `--max-iterations`,
`--max-fix-attempts`, `--auto-fix` (default true).

"Safe to auto-fix" = the PR is open, not conflicting, and within the fix-attempt
budget. Conflicts, merged/closed PRs, and exhausted budgets notify a human instead.
`data`: `{ pr, done }`.

## `status`
List worktrees, sessions, and PRs. Flags: `--prs`, `--prune`.

`data`: `{ worktrees[]:{worktree, session?, pr?, checks?}, generated_at }`.

## `tui` (or `shepherd` with no args)
Interactive Bubble Tea dashboard. Keys: `↑/↓` navigate, `/` filter, `enter` open a
session's live log, `f` toggle follow, `esc` back, `q` quit. (Requires a real
terminal; not usable with `--json`.)

## `update`
Self-update to the latest GitHub release: checks `releases/latest`, downloads the
matching archive, verifies its checksum, and atomically replaces the running
binary. Flags: `--check` (check only, don't install).

`data`: `{ current, latest, up_to_date, updated, asset?, path?, check_only }`.

## `version`
`data`: `{ version, commit, date, go, os, arch }`.
