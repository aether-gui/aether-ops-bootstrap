package ssh

import (
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var _ components.Component = (*Component)(nil)

func TestName(t *testing.T) {
	c := New("")
	if c.Name() != "ssh" {
		t.Errorf("Name() = %q, want %q", c.Name(), "ssh")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New("")
	m := &bundle.Manifest{BundleVersion: "2026.04.1"}
	if v := c.DesiredVersion(m); v != "2026.04.1" {
		t.Errorf("DesiredVersion = %q, want %q", v, "2026.04.1")
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New("")
	s := &state.State{
		Components: map[string]state.ComponentState{
			"ssh": {Version: "2026.03.1"},
		},
	}
	if v := c.CurrentVersion(s); v != "2026.03.1" {
		t.Errorf("CurrentVersion = %q, want %q", v, "2026.03.1")
	}
}

func TestPlanEmptyExtractDirReturnsNoOp(t *testing.T) {
	c := New("")
	plan, err := c.Plan("", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan with empty extractDir should return NoOp")
	}
}
