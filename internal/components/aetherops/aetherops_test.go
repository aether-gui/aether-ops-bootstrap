package aetherops

import (
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var _ components.Component = (*Component)(nil)

func TestName(t *testing.T) {
	c := New("", nil)
	if c.Name() != "aether_ops" {
		t.Errorf("Name() = %q, want %q", c.Name(), "aether_ops")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New("", nil)
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			AetherOps: &bundle.AetherOpsEntry{Version: "1.4.0"},
		},
	}
	if v := c.DesiredVersion(m); v != "1.4.0" {
		t.Errorf("DesiredVersion = %q, want %q", v, "1.4.0")
	}
}

func TestDesiredVersionNilAetherOps(t *testing.T) {
	c := New("", nil)
	m := &bundle.Manifest{}
	if v := c.DesiredVersion(m); v != "" {
		t.Errorf("DesiredVersion with nil AetherOps = %q, want empty", v)
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New("", nil)
	s := &state.State{
		Components: map[string]state.ComponentState{
			"aether_ops": {Version: "1.3.0"},
		},
	}
	if v := c.CurrentVersion(s); v != "1.3.0" {
		t.Errorf("CurrentVersion = %q, want %q", v, "1.3.0")
	}
}

func TestPlanNoOpWhenNilManifest(t *testing.T) {
	c := New("", nil)
	plan, err := c.Plan("", "1.4.0")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan should be NoOp when manifest is nil")
	}
}
