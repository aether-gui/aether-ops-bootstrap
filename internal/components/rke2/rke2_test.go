package rke2

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
	if c.Name() != "rke2" {
		t.Errorf("Name() = %q, want %q", c.Name(), "rke2")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New()
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			RKE2: &bundle.RKE2Entry{Version: "v1.33.1+rke2r1"},
		},
	}
	if v := c.DesiredVersion(m); v != "v1.33.1+rke2r1" {
		t.Errorf("DesiredVersion = %q, want %q", v, "v1.33.1+rke2r1")
	}
}

func TestDesiredVersionNilRKE2(t *testing.T) {
	c := New()
	m := &bundle.Manifest{}
	if v := c.DesiredVersion(m); v != "" {
		t.Errorf("DesiredVersion with nil RKE2 = %q, want empty", v)
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New()
	s := &state.State{
		Components: map[string]state.ComponentState{
			"rke2": {Version: "v1.32.0+rke2r1"},
		},
	}
	if v := c.CurrentVersion(s); v != "v1.32.0+rke2r1" {
		t.Errorf("CurrentVersion = %q, want %q", v, "v1.32.0+rke2r1")
	}
}

func TestPlanNotImplemented(t *testing.T) {
	c := New()
	_, err := c.Plan("", "v1.33.1+rke2r1")
	if !errors.Is(err, components.ErrNotImplemented) {
		t.Errorf("Plan error = %v, want ErrNotImplemented", err)
	}
}
