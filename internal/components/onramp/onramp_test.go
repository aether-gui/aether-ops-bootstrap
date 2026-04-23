package onramp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

func TestName(t *testing.T) {
	c := New("", nil)
	if c.Name() != "onramp" {
		t.Errorf("Name() = %q", c.Name())
	}
}

func TestDesiredVersion_NilManifest(t *testing.T) {
	c := New("", nil)
	if v := c.DesiredVersion(&bundle.Manifest{}); v != "" {
		t.Errorf("DesiredVersion on empty manifest = %q, want empty", v)
	}
}

func TestDesiredVersion_OnrampOnly(t *testing.T) {
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			Onramp: &bundle.OnrampEntry{ResolvedSHA: "deadbeef"},
		},
	}
	c := New("", m)
	if v := c.DesiredVersion(m); v != "onramp:deadbeef" {
		t.Errorf("DesiredVersion = %q", v)
	}
}

func TestDesiredVersion_Composite(t *testing.T) {
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			Onramp: &bundle.OnrampEntry{ResolvedSHA: "aaa"},
			HelmCharts: []bundle.HelmChartsEntry{
				{Name: "sdcore", ResolvedSHA: "bbb"},
				{Name: "ran", ResolvedSHA: "ccc"},
			},
		},
	}
	c := New("", m)
	want := "onramp:aaa,sdcore:bbb,ran:ccc"
	if v := c.DesiredVersion(m); v != want {
		t.Errorf("DesiredVersion = %q, want %q", v, want)
	}
}

func TestCurrentVersion(t *testing.T) {
	s := &state.State{
		Components: map[string]state.ComponentState{
			"onramp": {Version: "onramp:oldsha"},
		},
	}
	c := New("", nil)
	if v := c.CurrentVersion(s); v != "onramp:oldsha" {
		t.Errorf("CurrentVersion = %q", v)
	}
}

func TestPlan_NoOpWhenCurrentMatchesDesired(t *testing.T) {
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			Onramp: &bundle.OnrampEntry{ResolvedSHA: "same"},
		},
	}
	c := New("", m)
	p, err := c.Plan("onramp:same", "onramp:same")
	if err != nil {
		t.Fatal(err)
	}
	if !p.NoOp {
		t.Errorf("plan should be NoOp")
	}
}

func TestPlan_NoOpWhenNoComponents(t *testing.T) {
	m := &bundle.Manifest{}
	c := New("", m)
	p, err := c.Plan("", "")
	if err != nil {
		t.Fatal(err)
	}
	if !p.NoOp {
		t.Errorf("plan should be NoOp when no components present")
	}
}

func TestPlan_ActionsForOnrampAndCharts(t *testing.T) {
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			Onramp: &bundle.OnrampEntry{
				ResolvedSHA: "1111222233334444aaaabbbbccccdddd",
				Path:        "onramp/aether-onramp",
			},
			HelmCharts: []bundle.HelmChartsEntry{
				{Name: "sdcore", ResolvedSHA: "ffff", Path: "helm-charts/sdcore"},
			},
		},
	}
	c := New("", m)
	p, err := c.Plan("", c.DesiredVersion(m))
	if err != nil {
		t.Fatal(err)
	}
	if p.NoOp {
		t.Fatal("plan should not be NoOp")
	}
	// onramp install + one chart install + credential injection + chown step
	if len(p.Actions) != 4 {
		t.Fatalf("len(Actions) = %d, want 4 (onramp, chart, set creds, chown)", len(p.Actions))
	}
}

func TestExtractRepo(t *testing.T) {
	extractDir := t.TempDir()
	srcDir := filepath.Join(extractDir, "onramp", "aether-onramp")
	if err := os.MkdirAll(filepath.Join(srcDir, "roles", "k8s"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "Makefile"), []byte("all:\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "roles", "k8s", "main.yml"), []byte("role: k8s\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "aether-onramp")
	c := New(extractDir, nil)
	if err := c.extractRepo("onramp/aether-onramp", dest); err != nil {
		t.Fatalf("extractRepo: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "Makefile")); err != nil {
		t.Errorf("Makefile missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "roles", "k8s", "main.yml")); err != nil {
		t.Errorf("nested file missing: %v", err)
	}
}

func TestExtractRepo_CleansStaleContents(t *testing.T) {
	extractDir := t.TempDir()
	srcDir := filepath.Join(extractDir, "onramp", "aether-onramp")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "new.txt"), []byte("new\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "aether-onramp")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "stale.txt"), []byte("old\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := New(extractDir, nil)
	if err := c.extractRepo("onramp/aether-onramp", dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "stale.txt")); !os.IsNotExist(err) {
		t.Error("stale file should have been removed")
	}
}

func TestExtractRepo_MissingSourceReturnsError(t *testing.T) {
	c := New(t.TempDir(), nil)
	err := c.extractRepo("onramp/not-there", filepath.Join(t.TempDir(), "dest"))
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestApply_RunsAllActions(t *testing.T) {
	c := New("", nil)
	calls := 0
	plan := struct {
		run func() error
	}{run: func() error { calls++; return nil }}
	_ = plan

	// Build a plan by hand to avoid needing root for chown.
	err := c.Apply(context.Background(), pseudoPlan(func(ctx context.Context) error {
		calls++
		return nil
	}, func(ctx context.Context) error {
		calls++
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

// pseudoPlan is a small helper for building a Plan in tests without
// having to import the components package for its Action type here.
func pseudoPlan(fns ...func(ctx context.Context) error) components.Plan {
	p := components.Plan{}
	for _, fn := range fns {
		p.Actions = append(p.Actions, components.Action{Fn: fn})
	}
	return p
}
