package rke2

import (
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var _ components.Component = (*Component)(nil)

func TestStageAppImages_NilManifest(t *testing.T) {
	c := New("", nil)
	if err := c.stageAppImages(); err != nil {
		t.Errorf("stageAppImages with nil manifest should be no-op: %v", err)
	}
}

func TestStageAppImages_NoImagesEntry(t *testing.T) {
	m := &bundle.Manifest{}
	c := New("", m)
	if err := c.stageAppImages(); err != nil {
		t.Errorf("stageAppImages with nil Images entry should be no-op: %v", err)
	}
}

func TestStageAppImages_EmptyImagesSlice(t *testing.T) {
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			Images: &bundle.ImagesEntry{},
		},
	}
	c := New("", m)
	if err := c.stageAppImages(); err != nil {
		t.Errorf("stageAppImages with empty Images should be no-op: %v", err)
	}
}

func TestName(t *testing.T) {
	c := New("", nil)
	if c.Name() != "rke2" {
		t.Errorf("Name() = %q, want %q", c.Name(), "rke2")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New("", nil)
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
	c := New("", nil)
	m := &bundle.Manifest{}
	if v := c.DesiredVersion(m); v != "" {
		t.Errorf("DesiredVersion with nil RKE2 = %q, want empty", v)
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New("", nil)
	s := &state.State{
		Components: map[string]state.ComponentState{
			"rke2": {Version: "v1.32.0+rke2r1"},
		},
	}
	if v := c.CurrentVersion(s); v != "v1.32.0+rke2r1" {
		t.Errorf("CurrentVersion = %q, want %q", v, "v1.32.0+rke2r1")
	}
}

func TestPlanNilManifestReturnsNoOp(t *testing.T) {
	c := New("", nil)
	plan, err := c.Plan("", "v1.0.0")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan with nil manifest should return NoOp")
	}
}
