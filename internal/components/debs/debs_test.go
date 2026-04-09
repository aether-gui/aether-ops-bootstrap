package debs

import (
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var _ components.Component = (*Component)(nil)

func TestName(t *testing.T) {
	c := New("", nil)
	if c.Name() != "debs" {
		t.Errorf("Name() = %q, want %q", c.Name(), "debs")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New("", nil)
	m := &bundle.Manifest{
		BundleVersion: "2026.04.1",
		Components: bundle.ComponentList{
			Debs: []bundle.DebEntry{{Name: "git"}},
		},
	}
	if v := c.DesiredVersion(m); v != "2026.04.1" {
		t.Errorf("DesiredVersion = %q, want %q", v, "2026.04.1")
	}
}

func TestDesiredVersionEmpty(t *testing.T) {
	c := New("", nil)
	m := &bundle.Manifest{BundleVersion: "2026.04.1"}
	if v := c.DesiredVersion(m); v != "" {
		t.Errorf("DesiredVersion with no debs = %q, want empty", v)
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New("", nil)
	s := &state.State{
		Components: map[string]state.ComponentState{
			"debs": {Version: "2026.03.1"},
		},
	}
	if v := c.CurrentVersion(s); v != "2026.03.1" {
		t.Errorf("CurrentVersion = %q, want %q", v, "2026.03.1")
	}
}

func TestCurrentVersionMissing(t *testing.T) {
	c := New("", nil)
	s := &state.State{Components: map[string]state.ComponentState{}}
	if v := c.CurrentVersion(s); v != "" {
		t.Errorf("CurrentVersion missing = %q, want empty", v)
	}
}

func TestPlanNoOpWhenUpToDate(t *testing.T) {
	c := New("", &bundle.Manifest{BundleVersion: "2026.04.1"})
	plan, err := c.Plan("2026.04.1", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan should be NoOp when current == desired")
	}
}
