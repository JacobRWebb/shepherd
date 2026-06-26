# AGENTS.md — driving Shepherd from an agent

This file tells an automated agent (e.g. Claude Code) how to use the `shepherd`
CLI. Humans should read [README.md](README.md).

## Golden rules

1. **Always pass `--json`.** Every command emits a single JSON object (or NDJSON
   for streams) on **stdout**; logs/diagnostics go to **stderr**. So
   `shepherd <cmd> --json` is safe to pipe to `jq`.
2. **Parse the envelope.** Success is `{"ok": true, "data": <result>}`; failure is
   `{"ok": false, "error": {"code": "...", "message": "..."}}`.
3. **Check the exit code** (below) before trusting output.
4. **Work happens in worktrees**, never the main tree. Use the `worktree.path`
   returned by `new` as the working directory for follow-up edits.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | generic error |
| 2 | invalid input (`invalid_input`) |
| 3 | not found (`not_found`) |
| 4 | not a git repository (`not_git_repo`) |

The `error.code` field mirrors these: `invalid_input`, `not_found`, `conflict`,
`not_git_repo`, `dirty`, `unsupported`, `internal`.

## Recipes

Create an isolated workspace and run an agent to completion:
```sh
shepherd new "#123" --headless --json
# => data.worktree.{name,path,branch}, data.task, data.result.{text,is_error,exit_code}
```

Launch a background agent and return immediately:
```sh
shepherd new "add metrics endpoint" --detach --json
# => data.session.{name,state,...}; poll with `shepherd status --json`
```

Run the validation gate without pushing:
```sh
shepherd ship <branch> --no-push --json
# => data.gate_passed (bool), data.pipeline.steps[]
```

Validate, push, and open a PR (self-healing):
```sh
shepherd ship <branch> --auto-fix --json
# => data.pushed, data.pr.{number,url,state}, data.fix_attempts
```

Fan out parallel agents:
```sh
shepherd crew "migrate to the new API" -n 4 --json
# => data.crew_id, data.agents[].{task,branch,state,summary,diffstat}
```

Keep a PR green:
```sh
shepherd babysit 42 --json
```

Inspect everything:
```sh
shepherd status --prs --json
# => data.worktrees[].{worktree, session, pr, checks}
```

Stay current:
```sh
shepherd update --check --json   # => data.{current,latest,up_to_date}
shepherd update                  # install the latest release over this binary
```

## Notes

- `new`/`crew` require the `claude` CLI; `ship`/`babysit` PRs require a configured
  forge (`gh` for GitHub, env-provided API token for Bitbucket).
- A non-numeric issue ref on GitHub returns `unsupported`; treat the arg as a
  branch name instead.
- `--json` implies headless for `new` (no interactive terminal takeover).
