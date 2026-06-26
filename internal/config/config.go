// Package config loads and validates Shepherd configuration.
//
// It is the only package that reads config files or SHEPHERD_* environment
// variables; the resulting Config is passed explicitly to the rest of the app
// (no globals). Precedence, low to high: built-in defaults, then the
// .shepherd.yaml file, then SHEPHERD_* env vars (with __ marking nesting),
// then any overrides the CLI applies imperatively.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	goyaml "go.yaml.in/yaml/v3"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

// Config is the fully-resolved Shepherd configuration.
type Config struct {
	Version       int                 `koanf:"version" yaml:"version"`
	Worktrees     WorktreesConfig     `koanf:"worktrees" yaml:"worktrees"`
	Forge         ForgeConfig         `koanf:"forge" yaml:"forge"`
	Session       SessionConfig       `koanf:"session" yaml:"session"`
	Claude        ClaudeConfig        `koanf:"claude" yaml:"claude"`
	Validation    ValidationConfig    `koanf:"validation" yaml:"validation"`
	Notifications NotificationsConfig `koanf:"notifications" yaml:"notifications"`
	Logging       LoggingConfig       `koanf:"logging" yaml:"logging"`

	// SourcePath is the file this config was loaded from ("" if pure defaults).
	// Not serialized; set by Load so commands can report provenance.
	SourcePath string `koanf:"-" yaml:"-"`
}

type WorktreesConfig struct {
	Root         string `koanf:"root" yaml:"root"`
	NameTemplate string `koanf:"name_template" yaml:"name_template"`
	BranchPrefix string `koanf:"branch_prefix" yaml:"branch_prefix"`
	BaseBranch   string `koanf:"base_branch" yaml:"base_branch"`
	AutoCleanup  bool   `koanf:"auto_cleanup" yaml:"auto_cleanup"`
}

type ForgeConfig struct {
	Provider  string          `koanf:"provider" yaml:"provider"` // github | bitbucket
	GitHub    GitHubConfig    `koanf:"github" yaml:"github"`
	Bitbucket BitbucketConfig `koanf:"bitbucket" yaml:"bitbucket"`
}

type GitHubConfig struct {
	Host             string   `koanf:"host" yaml:"host"`
	DefaultReviewers []string `koanf:"default_reviewers" yaml:"default_reviewers"`
	DraftPRs         bool     `koanf:"draft_prs" yaml:"draft_prs"`
}

type BitbucketConfig struct {
	BaseURL          string   `koanf:"base_url" yaml:"base_url"`
	Workspace        string   `koanf:"workspace" yaml:"workspace"`
	RepoSlug         string   `koanf:"repo_slug" yaml:"repo_slug"`
	EmailEnv         string   `koanf:"email_env" yaml:"email_env"`
	TokenEnv         string   `koanf:"token_env" yaml:"token_env"`
	DefaultReviewers []string `koanf:"default_reviewers" yaml:"default_reviewers"`
}

type SessionConfig struct {
	Backend string              `koanf:"backend" yaml:"backend"` // auto | tmux | native
	Tmux    TmuxSessionConfig   `koanf:"tmux" yaml:"tmux"`
	Native  NativeSessionConfig `koanf:"native" yaml:"native"`
}

type TmuxSessionConfig struct {
	SocketName    string `koanf:"socket_name" yaml:"socket_name"`
	SessionPrefix string `koanf:"session_prefix" yaml:"session_prefix"`
	WSL           bool   `koanf:"wsl" yaml:"wsl"`
}

type NativeSessionConfig struct {
	LogDir string `koanf:"log_dir" yaml:"log_dir"`
	Detach bool   `koanf:"detach" yaml:"detach"`
}

type ClaudeConfig struct {
	Binary                     string               `koanf:"binary" yaml:"binary"`
	Model                      string               `koanf:"model" yaml:"model"`
	FallbackModel              string               `koanf:"fallback_model" yaml:"fallback_model"`
	PermissionMode             string               `koanf:"permission_mode" yaml:"permission_mode"`
	DangerouslySkipPermissions bool                 `koanf:"dangerously_skip_permissions" yaml:"dangerously_skip_permissions"`
	Effort                     string               `koanf:"effort" yaml:"effort"`
	AddDirs                    []string             `koanf:"add_dirs" yaml:"add_dirs"`
	AppendSystemPrompt         string               `koanf:"append_system_prompt" yaml:"append_system_prompt"`
	AllowedTools               []string             `koanf:"allowed_tools" yaml:"allowed_tools"`
	DisallowedTools            []string             `koanf:"disallowed_tools" yaml:"disallowed_tools"`
	Headless                   ClaudeHeadlessConfig `koanf:"headless" yaml:"headless"`
	ExtraArgs                  []string             `koanf:"extra_args" yaml:"extra_args"`
}

type ClaudeHeadlessConfig struct {
	OutputFormat   string  `koanf:"output_format" yaml:"output_format"` // text|json|stream-json
	MaxBudgetUSD   float64 `koanf:"max_budget_usd" yaml:"max_budget_usd"`
	TimeoutSeconds int     `koanf:"timeout_seconds" yaml:"timeout_seconds"`
}

type ValidationConfig struct {
	StopOnFailure  bool              `koanf:"stop_on_failure" yaml:"stop_on_failure"`
	DefaultTimeout string            `koanf:"default_timeout" yaml:"default_timeout"`
	Env            map[string]string `koanf:"env" yaml:"env,omitempty"`
	Steps          []ValidationStep  `koanf:"steps" yaml:"steps"`
}

type ValidationStep struct {
	Name            string `koanf:"name" yaml:"name"`
	Run             string `koanf:"run" yaml:"run"`
	Timeout         string `koanf:"timeout" yaml:"timeout,omitempty"`
	ContinueOnError bool   `koanf:"continue_on_error" yaml:"continue_on_error,omitempty"`
	WorkdirRel      string `koanf:"workdir" yaml:"workdir,omitempty"`
}

type NotificationsConfig struct {
	Enabled  bool          `koanf:"enabled" yaml:"enabled"`
	OnEvents []string      `koanf:"on_events" yaml:"on_events"`
	Channels []string      `koanf:"channels" yaml:"channels"` // terminal | webhook | command
	Webhook  WebhookConfig `koanf:"webhook" yaml:"webhook"`
	Command  string        `koanf:"command" yaml:"command"`
}

type WebhookConfig struct {
	URLEnv string `koanf:"url_env" yaml:"url_env"`
	Format string `koanf:"format" yaml:"format"` // slack | json
}

type LoggingConfig struct {
	Level  string `koanf:"level" yaml:"level"`   // debug|info|warn|error
	Format string `koanf:"format" yaml:"format"` // console|json
	File   string `koanf:"file" yaml:"file"`
}

// Load resolves defaults, then the config file (explicitPath or auto-discovered),
// then SHEPHERD_* env vars, into a validated Config. An explicitPath that does
// not exist is an error; auto-discovery silently falls back to defaults+env.
func Load(explicitPath string) (Config, error) {
	c := DefaultConfig()

	path := explicitPath
	if path == "" {
		path = discover()
	}

	k := koanf.New(".")
	if path != "" {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
				return Config{}, fmt.Errorf("reading config %s: %w", path, err)
			}
		} else if explicitPath != "" {
			return Config{}, domain.InvalidInputf("config file not found: %s", explicitPath)
		} else {
			path = "" // discovered path vanished between Stat and use
		}
	}

	if err := k.Load(env.Provider("SHEPHERD_", ".", envKey), nil); err != nil {
		return Config{}, fmt.Errorf("reading SHEPHERD_* env: %w", err)
	}

	// Overlay file+env onto the defaults struct. mapstructure only sets keys
	// that are present, so unspecified fields keep their default values.
	if err := k.Unmarshal("", &c); err != nil {
		return Config{}, fmt.Errorf("decoding config: %w", err)
	}

	c.SourcePath = path
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// envKey maps SHEPHERD_CLAUDE__MODEL -> claude.model.
func envKey(s string) string {
	s = strings.TrimPrefix(s, "SHEPHERD_")
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, "__", ".")
}

// discover walks up from the working directory looking for .shepherd.yaml, then
// falls back to the per-user config dir. Returns "" if none found.
func discover() string {
	if dir, err := os.Getwd(); err == nil {
		for {
			p := filepath.Join(dir, ".shepherd.yaml")
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if cdir, err := os.UserConfigDir(); err == nil {
		p := filepath.Join(cdir, "shepherd", "config.yaml")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// Validate enforces enum and relationship invariants, returning all problems at
// once (wrapped as ErrInvalidInput).
func (c Config) Validate() error {
	var problems []string
	oneOf := func(name, val string, allowed ...string) {
		for _, a := range allowed {
			if val == a {
				return
			}
		}
		problems = append(problems, fmt.Sprintf("%s=%q must be one of %s", name, val, strings.Join(allowed, "|")))
	}

	oneOf("forge.provider", c.Forge.Provider, "github", "bitbucket")
	oneOf("session.backend", c.Session.Backend, "auto", "tmux", "native")
	oneOf("claude.permission_mode", c.Claude.PermissionMode,
		"default", "plan", "acceptEdits", "dontAsk", "auto", "bypassPermissions")
	oneOf("claude.headless.output_format", c.Claude.Headless.OutputFormat, "text", "json", "stream-json")
	oneOf("logging.level", c.Logging.Level, "debug", "info", "warn", "error")
	oneOf("logging.format", c.Logging.Format, "console", "json")
	if c.Claude.Effort != "" {
		oneOf("claude.effort", c.Claude.Effort, "low", "medium", "high", "xhigh", "max")
	}

	if c.Forge.Provider == "bitbucket" && strings.TrimSpace(c.Forge.Bitbucket.Workspace) == "" {
		problems = append(problems, "forge.bitbucket.workspace is required when provider=bitbucket")
	}

	if c.Validation.DefaultTimeout != "" {
		if _, err := time.ParseDuration(c.Validation.DefaultTimeout); err != nil {
			problems = append(problems, fmt.Sprintf("validation.default_timeout=%q: %v", c.Validation.DefaultTimeout, err))
		}
	}
	for i, s := range c.Validation.Steps {
		if strings.TrimSpace(s.Run) == "" {
			problems = append(problems, fmt.Sprintf("validation.steps[%d].run is empty", i))
		}
		if s.Timeout != "" {
			if _, err := time.ParseDuration(s.Timeout); err != nil {
				problems = append(problems, fmt.Sprintf("validation.steps[%d].timeout=%q: %v", i, s.Timeout, err))
			}
		}
	}

	if len(problems) > 0 {
		return domain.InvalidInputf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// Marshal renders the config as YAML (for provenance/debugging).
func (c Config) Marshal() ([]byte, error) { return goyaml.Marshal(c) }
