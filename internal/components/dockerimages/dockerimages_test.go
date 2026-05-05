package dockerimages

import (
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var _ components.Component = (*Component)(nil)

func TestName(t *testing.T) {
	c := New("", nil)
	if c.Name() != "dockerimages" {
		t.Errorf("Name() = %q, want %q", c.Name(), "dockerimages")
	}
}

func TestDesiredVersionEmptyWhenNoImages(t *testing.T) {
	c := New("", nil)

	// Manifest with no Images entry: empty.
	if v := c.DesiredVersion(&bundle.Manifest{BundleVersion: "1.0"}); v != "" {
		t.Errorf("DesiredVersion with no Images = %q, want empty", v)
	}

	// Empty Images list: empty (so Plan stays a NoOp).
	m := &bundle.Manifest{
		BundleVersion: "1.0",
		Components:    bundle.ComponentList{Images: &bundle.ImagesEntry{}},
	}
	if v := c.DesiredVersion(m); v != "" {
		t.Errorf("DesiredVersion with empty Images = %q, want empty", v)
	}
}

func TestDesiredVersionReturnsBundleVersion(t *testing.T) {
	c := New("", nil)
	m := &bundle.Manifest{
		BundleVersion: "2026.05.05.4",
		Components: bundle.ComponentList{
			Images: &bundle.ImagesEntry{
				Images: []bundle.ImageArtifact{
					{Ref: "ghcr.io/example/x:v1", Path: "images/x.tar"},
				},
			},
		},
	}
	if v := c.DesiredVersion(m); v != "2026.05.05.4" {
		t.Errorf("DesiredVersion = %q, want %q", v, "2026.05.05.4")
	}
}

func TestPlanNoOpWhenAtDesired(t *testing.T) {
	m := &bundle.Manifest{
		BundleVersion: "1.0",
		Components: bundle.ComponentList{
			Images: &bundle.ImagesEntry{
				Images: []bundle.ImageArtifact{{Ref: "x:v1", Path: "images/x.tar"}},
			},
		},
	}
	c := New("", m)
	plan, err := c.Plan("1.0", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.NoOp || len(plan.Actions) != 0 {
		t.Errorf("expected NoOp plan when current==desired, got %+v", plan)
	}
}

func TestPlanProducesActionWhenWorkPending(t *testing.T) {
	m := &bundle.Manifest{
		BundleVersion: "1.0",
		Components: bundle.ComponentList{
			Images: &bundle.ImagesEntry{
				Images: []bundle.ImageArtifact{
					{Ref: "x:v1", Path: "images/x.tar"},
					{Ref: "y:v2", Path: "images/y.tar"},
				},
			},
		},
	}
	c := New("/tmp/extract", m)
	plan, err := c.Plan("", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if plan.NoOp {
		t.Fatal("expected non-NoOp plan when there's work to do")
	}
	if len(plan.Actions) != 1 {
		t.Errorf("expected 1 batched action, got %d", len(plan.Actions))
	}
}

func TestCurrentVersionFromState(t *testing.T) {
	c := New("", nil)
	s := &state.State{
		Components: map[string]state.ComponentState{
			"dockerimages": {Version: "1.0"},
		},
	}
	if v := c.CurrentVersion(s); v != "1.0" {
		t.Errorf("CurrentVersion = %q, want %q", v, "1.0")
	}
}
