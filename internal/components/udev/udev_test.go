package udev

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var _ components.Component = (*Component)(nil)

func TestName(t *testing.T) {
	c := New("")
	if c.Name() != "udev" {
		t.Errorf("Name() = %q, want %q", c.Name(), "udev")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New("")
	m := &bundle.Manifest{BundleVersion: "2026.05.06.1"}
	if v := c.DesiredVersion(m); v != "2026.05.06.1" {
		t.Errorf("DesiredVersion = %q, want %q", v, "2026.05.06.1")
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New("")
	s := &state.State{
		Components: map[string]state.ComponentState{
			"udev": {Version: "2026.04.1"},
		},
	}
	if v := c.CurrentVersion(s); v != "2026.04.1" {
		t.Errorf("CurrentVersion = %q, want %q", v, "2026.04.1")
	}
}

func TestPlanNoOpWhenVersionsMatch(t *testing.T) {
	c := New("")
	plan, err := c.Plan("2026.04.1", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan should be NoOp when current==desired")
	}
}

func TestPlanNoOpWhenTemplateDirMissing(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	plan, err := c.Plan("", "2026.05.06.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan should be NoOp when template dir is missing")
	}
}

func TestPlanNoOpWhenTemplateDirEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates", "udev.rules.d"), 0755); err != nil {
		t.Fatal(err)
	}
	c := New(dir)
	plan, err := c.Plan("", "2026.05.06.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan should be NoOp when template dir has no rule files")
	}
}

func TestPlanProducesActionsForRuleFiles(t *testing.T) {
	dir := t.TempDir()
	rulesSrc := filepath.Join(dir, "templates", "udev.rules.d")
	if err := os.MkdirAll(rulesSrc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesSrc, "uhd-usrp.rules"), []byte("rule\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := New(dir)
	plan, err := c.Plan("", "2026.05.06.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.NoOp {
		t.Fatal("Plan should not be NoOp when rule files exist")
	}
	if len(plan.Actions) != 2 {
		t.Errorf("expected 2 actions (install + reload), got %d", len(plan.Actions))
	}
}
