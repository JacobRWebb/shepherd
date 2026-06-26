package config

import (
	"strings"
	"testing"
)

func TestDefaultConfigValid(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	c := DefaultConfig()
	c.Forge.Provider = "gitlab"
	c.Session.Backend = "screen"
	c.Logging.Level = "loud"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"forge.provider", "session.backend", "logging.level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestBitbucketRequiresWorkspace(t *testing.T) {
	c := DefaultConfig()
	c.Forge.Provider = "bitbucket"
	c.Forge.Bitbucket.Workspace = ""
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Errorf("expected workspace error, got %v", err)
	}
}
