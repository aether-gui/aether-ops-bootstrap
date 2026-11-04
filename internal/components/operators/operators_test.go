package operators

import (
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

func TestName(t *testing.T) {
	c := New("/tmp/extract", nil)
	if got := c.Name(); got != "operators" {
		t.Errorf("Name() = %q, want %q", got, "operators")
	}
}

func TestDesiredVersion_NoManifest(t *testing.T) {
	c := New("/tmp/extract", nil)
	if got := c.DesiredVersion(nil); got != "" {
		t.Errorf("DesiredVersion with nil manifest = %q, want empty", got)
	}
}

func TestDesiredVersion_NoOperatorCharts(t *testing.T) {
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			HelmCharts: []bundle.HelmChartsEntry{
				{Name: "sdcore-helm-charts", ResolvedSHA: "abc123def456"},
			},
		},
	}
	c := New("/tmp/extract", m)
	if got := c.DesiredVersion(m); got != "" {
		t.Errorf("DesiredVersion with only sdcore chart = %q, want empty", got)
	}
}

func TestDesiredVersion_WithOperatorCharts(t *testing.T) {
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			HelmCharts: []bundle.HelmChartsEntry{
				{Name: "sdcore-helm-charts", ResolvedSHA: "abc123def456"},
				{Name: "mongodb-kubernetes", ResolvedSHA: "aaa111bbb222ccc333"},
				{Name: "strimzi-kafka-operator", ResolvedSHA: "ddd444eee555fff666"},
			},
		},
	}
	c := New("/tmp/extract", m)
	got := c.DesiredVersion(m)
	want := "mongodb-kubernetes=aaa111bbb222,strimzi-kafka-operator=ddd444eee555"
	if got != want {
		t.Errorf("DesiredVersion = %q, want %q", got, want)
	}
}

func TestCurrentVersion_Empty(t *testing.T) {
	c := New("/tmp/extract", nil)
	s := &state.State{}
	if got := c.CurrentVersion(s); got != "" {
		t.Errorf("CurrentVersion on empty state = %q, want empty", got)
	}
}

func TestCurrentVersion_Recorded(t *testing.T) {
	c := New("/tmp/extract", nil)
	s := &state.State{
		Components: map[string]state.ComponentState{
			"operators": {Version: "v1"},
		},
	}
	if got := c.CurrentVersion(s); got != "v1" {
		t.Errorf("CurrentVersion = %q, want %q", got, "v1")
	}
}

func TestPlan_NoOp_NoCharts(t *testing.T) {
	c := New("/tmp/extract", &bundle.Manifest{})
	plan, err := c.Plan("", "")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.NoOp {
		t.Error("expected NoOp when desired version is empty")
	}
}

func TestPlan_NoOp_SameVersion(t *testing.T) {
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			HelmCharts: []bundle.HelmChartsEntry{
				{Name: "mongodb-kubernetes", ResolvedSHA: "aaa111bbb222ccc333"},
			},
		},
	}
	c := New("/tmp/extract", m)
	plan, err := c.Plan("mongodb-kubernetes=aaa111bbb222", "mongodb-kubernetes=aaa111bbb222")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.NoOp {
		t.Error("expected NoOp when current == desired")
	}
}
