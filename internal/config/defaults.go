package config

// DefaultConfig returns the built-in baseline configuration. The .shepherd.yaml
// file and SHEPHERD_* env vars are overlaid on top of this. The default
// validation steps assume a Go project; users tailor them per repo via `init`.
func DefaultConfig() Config {
	return Config{
		Version: 1,
		Worktrees: WorktreesConfig{
			Root:         "../.shepherd-worktrees",
			NameTemplate: "{slug}",
			BranchPrefix: "shepherd/",
			BaseBranch:   "",
			AutoCleanup:  true,
		},
		Forge: ForgeConfig{
			Provider: "github",
			GitHub:   GitHubConfig{Host: "github.com"},
			Bitbucket: BitbucketConfig{
				BaseURL:  "https://api.bitbucket.org/2.0",
				EmailEnv: "BITBUCKET_EMAIL",
				TokenEnv: "BITBUCKET_API_TOKEN",
			},
		},
		Session: SessionConfig{
			Backend: "auto",
			Tmux:    TmuxSessionConfig{SocketName: "shepherd", SessionPrefix: "shp-"},
			Native:  NativeSessionConfig{LogDir: ".shepherd/logs", Detach: true},
		},
		Claude: ClaudeConfig{
			Binary:         "claude",
			PermissionMode: "default",
			Headless: ClaudeHeadlessConfig{
				OutputFormat:   "json",
				TimeoutSeconds: 1800,
			},
		},
		Validation: ValidationConfig{
			StopOnFailure:  true,
			DefaultTimeout: "5m",
			Steps: []ValidationStep{
				{Name: "build", Run: "go build ./..."},
				{Name: "test", Run: "go test ./...", Timeout: "10m"},
				{Name: "vet", Run: "go vet ./..."},
			},
		},
		Notifications: NotificationsConfig{
			Enabled:  false,
			OnEvents: []string{"pr_opened", "pr_merged", "validation_failed", "agent_finished"},
			Channels: []string{"terminal"},
			Webhook:  WebhookConfig{URLEnv: "SHEPHERD_WEBHOOK_URL", Format: "slack"},
		},
		Logging: LoggingConfig{Level: "info", Format: "console"},
	}
}
