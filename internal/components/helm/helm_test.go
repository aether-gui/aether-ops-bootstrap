package helm

import (
	"errors"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var _ components.Component = (*Component)(nil)

func TestName(t *testing.T) {
	c := New()
	if c.Name() != "helm" {
		t.Errorf("Name() = %q, want %q", c.Name(), "helm")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New()
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			Helm: &bundle.HelmEntry{Version: "v3.17.3"},
		},
	}
	if v := c.DesiredVersion(m); v != "v3.17.3" {
		t.Errorf("DesiredVersion = %q, want %q", v, "v3.17.3")
	}
}

func TestDesiredVersionNilHelm(t *testing.T) {
	c := New()
	m := &bundle.Manifest{}
	if v := c.DesiredVersion(m); v != "" {
		t.Errorf("DesiredVersion with nil Helm = %q, want empty", v)
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New()
	s := &state.State{
		Components: map[string]state.ComponentState{
			"helm": {Version: "v3.16.0"},
		},
	}
	if v := c.CurrentVersion(s); v != "v3.16.0" {
		t.Errorf("CurrentVersion = %q, want %q", v, "v3.16.0")
	}
}

func TestPlanNotImplemented(t *testing.T) {
	c := New()
	_, err := c.Plan("", "v3.17.3")
	if !errors.Is(err, components.ErrNotImplemented) {
		t.Errorf("Plan error = %v, want ErrNotImplemented", err)
	}
}
