package agent

import (
	"strconv"
	"strings"
)

// buildBase assembles the flags common to interactive and headless runs, merging
// the Spec over the ClaudeConfig defaults.
func (c *Claude) buildBase(s Spec) []string {
	var a []string

	if m := pick(s.Model, c.cfg.Model); m != "" {
		a = append(a, "--model", m)
	}

	// Permissions: skip-flag and permission-mode are mutually exclusive.
	if s.SkipPermissions || c.cfg.DangerouslySkipPermissions {
		a = append(a, "--dangerously-skip-permissions")
	} else if pm := pick(s.PermissionMode, c.cfg.PermissionMode); pm != "" {
		a = append(a, "--permission-mode", pm)
	}

	if e := pick(s.Effort, c.cfg.Effort); e != "" {
		a = append(a, "--effort", e)
	}

	for _, d := range dedupe(append(append([]string{}, c.cfg.AddDirs...), s.AddDirs...)) {
		a = append(a, "--add-dir", d)
	}

	if asp := pick(s.AppendSystemPrompt, c.cfg.AppendSystemPrompt); asp != "" {
		a = append(a, "--append-system-prompt", asp)
	}

	if tools := orDefault(s.AllowedTools, c.cfg.AllowedTools); len(tools) > 0 {
		a = append(a, "--allowed-tools", strings.Join(tools, " "))
	}
	if tools := orDefault(s.DisallowedTools, c.cfg.DisallowedTools); len(tools) > 0 {
		a = append(a, "--disallowed-tools", strings.Join(tools, " "))
	}

	if s.ContinueSession {
		a = append(a, "--continue")
	}
	if s.ResumeSessionID != "" {
		a = append(a, "--resume", s.ResumeSessionID)
	}
	if s.SessionID != "" {
		a = append(a, "--session-id", s.SessionID)
	}
	if s.ForkSession {
		a = append(a, "--fork-session")
	}

	a = append(a, c.cfg.ExtraArgs...)
	a = append(a, s.ExtraArgs...)
	return a
}

// InteractiveArgs returns the argv for an interactive run (prompt is the final
// positional argument, if any).
func (c *Claude) InteractiveArgs(spec InteractiveSpec) []string {
	a := c.buildBase(spec.Spec)
	if spec.Prompt != "" {
		a = append(a, spec.Prompt)
	}
	return a
}

// HeadlessArgs returns the argv for a `claude -p` run.
func (c *Claude) HeadlessArgs(spec HeadlessSpec) []string {
	format := pick(spec.OutputFormat, c.cfg.Headless.OutputFormat)
	if format == "" {
		format = "json"
	}
	a := []string{"-p", "--output-format", format}
	if format == "stream-json" {
		a = append(a, "--verbose") // stream-json in print mode requires --verbose
		if spec.StreamHandler != nil {
			a = append(a, "--include-partial-messages")
		}
	}
	if fb := pick(spec.FallbackModel, c.cfg.FallbackModel); fb != "" {
		a = append(a, "--fallback-model", fb)
	}
	budget := spec.MaxBudgetUSD
	if budget == 0 {
		budget = c.cfg.Headless.MaxBudgetUSD
	}
	if budget > 0 {
		a = append(a, "--max-budget-usd", strconv.FormatFloat(budget, 'f', -1, 64))
	}
	a = append(a, c.buildBase(spec.Spec)...)
	if spec.Prompt != "" {
		a = append(a, spec.Prompt)
	}
	return a
}

func pick(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func orDefault(v, fallback []string) []string {
	if len(v) > 0 {
		return v
	}
	return fallback
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
