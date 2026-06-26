# Configuration

Shepherd reads `.shepherd.yaml`, discovered by walking up from the working
directory (then the per-user config dir). Precedence, low → high:

1. built-in defaults
2. `.shepherd.yaml`
3. `SHEPHERD_*` environment variables (`__` separates nesting)
4. CLI flags (e.g. `--worktrees-root`)

Example: `SHEPHERD_CLAUDE__MODEL=opus` overrides `claude.model`.

## Sections

### `worktrees`
| Key | Default | Notes |
|---|---|---|
| `root` | `../.shepherd-worktrees` | where worktrees are created (relative to repo root) |
| `name_template` | `{slug}` | `{slug}` from the task, `{n}` short-uuid on collision |
| `branch_prefix` | `shepherd/` | branch = prefix + dir name |
| `base_branch` | `""` | empty = repo default branch (origin/HEAD) |
| `auto_cleanup` | `true` | remove merged worktrees on `status --prune` |

### `forge`
`provider: github | bitbucket`.

- **github**: `host` (for Enterprise), `default_reviewers`, `draft_prs`. Auth is `gh`'s
  own (`gh auth login` / `GH_TOKEN`).
- **bitbucket**: `base_url`, `workspace`, `repo_slug`, `email_env`, `token_env`,
  `default_reviewers`. Auth is an **Atlassian API token** (account email + token over
  HTTP Basic). The config holds env-var *names*; the secrets live in those env vars.
  App passwords are not supported (retired June 2026).

### `session`
`backend: auto | tmux | native`. `auto` picks tmux on non-Windows hosts where tmux is
on `PATH`, else native.

- **tmux**: `socket_name`, `session_prefix`.
- **native**: `log_dir`, `detach`.

### `claude`
`binary`, `model`, `fallback_model`, `permission_mode`
(`default|plan|acceptEdits|dontAsk|auto|bypassPermissions`),
`dangerously_skip_permissions`, `effort` (`low|medium|high|xhigh|max`), `add_dirs`,
`append_system_prompt`, `allowed_tools`, `disallowed_tools`,
`headless.{output_format,max_budget_usd,timeout_seconds}`, `extra_args`.

### `validation`
The clean push gate. `stop_on_failure`, `default_timeout`, `env`, and `steps[]` where
each step is `{name, run, timeout?, continue_on_error?, workdir?}`. Steps run in the
worktree via the platform shell. `continue_on_error` steps are advisory (they don't
block the gate).

### `notifications`
`enabled`, `on_events`, `channels` (`terminal|webhook|command`),
`webhook.{url_env,format}` (`slack|json`), `command` (receives the event JSON on stdin).

### `logging`
`level` (`debug|info|warn|error`), `format` (`console|json`), `file` (extra JSON sink).

## Secrets

Never put secrets in `.shepherd.yaml`. Reference them by env-var name
(`token_env`, `email_env`, `webhook.url_env`).
