package config

import _ "embed"

// DefaultConfigYAML is the commented .shepherd.yaml template written verbatim by
// `shepherd init`.
//
//go:embed templates/shepherd.yaml
var DefaultConfigYAML string

// SkillTemplate is the Claude Code skill written to skills/shepherd/SKILL.md by
// `shepherd init`.
//
//go:embed templates/SKILL.md
var SkillTemplate string
