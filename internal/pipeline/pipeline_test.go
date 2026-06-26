package pipeline

import (
	"context"
	"testing"

	"github.com/JacobRWebb/shepherd/internal/config"
)

func TestFromConfig(t *testing.T) {
	c, err := FromConfig(config.ValidationConfig{
		DefaultTimeout: "30s",
		Steps:          []config.ValidationStep{{Name: "a", Run: "echo hi", Timeout: "5s"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultTimeout.String() != "30s" {
		t.Errorf("default timeout = %v", c.DefaultTimeout)
	}
	if len(c.Steps) != 1 || c.Steps[0].Timeout.String() != "5s" {
		t.Errorf("steps = %+v", c.Steps)
	}
}

func TestFromConfigBadDuration(t *testing.T) {
	if _, err := FromConfig(config.ValidationConfig{DefaultTimeout: "notaduration"}); err == nil {
		t.Errorf("expected duration parse error")
	}
}

func TestGateStopsOnFailure(t *testing.T) {
	cfg := Config{
		StopOnFailure: true,
		Steps: []Step{
			{Name: "ok", Run: "exit 0"},
			{Name: "bad", Run: "exit 1"},
			{Name: "never", Run: "exit 0"},
		},
	}
	allowed, res, err := NewRunner(cfg, nil).Gate(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Errorf("gate should fail")
	}
	if res.Steps[0].Status != StepPassed {
		t.Errorf("step0 = %v", res.Steps[0].Status)
	}
	if res.Steps[1].Status != StepFailed {
		t.Errorf("step1 = %v", res.Steps[1].Status)
	}
	if res.Steps[2].Status != StepSkipped {
		t.Errorf("step2 = %v (expected skipped on stop_on_failure)", res.Steps[2].Status)
	}
}

func TestContinueOnErrorDoesNotBlockGate(t *testing.T) {
	cfg := Config{
		StopOnFailure: true,
		Steps: []Step{
			{Name: "advisory", Run: "exit 1", ContinueOnError: true},
			{Name: "ok", Run: "exit 0"},
		},
	}
	allowed, res, err := NewRunner(cfg, nil).Gate(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Errorf("advisory failure should not block the gate; result = %+v", res)
	}
}
