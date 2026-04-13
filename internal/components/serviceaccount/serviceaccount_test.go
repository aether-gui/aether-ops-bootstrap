package serviceaccount

import (
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var _ components.Component = (*Component)(nil)

func TestName(t *testing.T) {
	c := New()
	if c.Name() != "service_account" {
		t.Errorf("Name() = %q, want %q", c.Name(), "service_account")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New()
	m := &bundle.Manifest{BundleVersion: "2026.04.1"}
	if v := c.DesiredVersion(m); v != "2026.04.1" {
		t.Errorf("DesiredVersion = %q, want %q", v, "2026.04.1")
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New()
	s := &state.State{
		Components: map[string]state.ComponentState{
			"service_account": {Version: "2026.03.1"},
		},
	}
	if v := c.CurrentVersion(s); v != "2026.03.1" {
		t.Errorf("CurrentVersion = %q, want %q", v, "2026.03.1")
	}
}

func TestPlanReturnsActions(t *testing.T) {
	c := New()
	plan, err := c.Plan("", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Error("Plan should return actions for new install")
	}
}
