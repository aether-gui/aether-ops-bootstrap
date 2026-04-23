package serviceaccount

import (
	"strings"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

var _ components.Component = (*Component)(nil)

func TestName(t *testing.T) {
	c := New()
	if c.Name() != "service_account" {
		t.Errorf("Name() = %q, want %q", c.Name(), "service_account")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New()
	m := &bundle.Manifest{BundleVersion: "2026.04.1"}
	if v := c.DesiredVersion(m); v != "2026.04.1" {
		t.Errorf("DesiredVersion = %q, want %q", v, "2026.04.1")
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New()
	s := &state.State{
		Components: map[string]state.ComponentState{
			"service_account": {Version: "2026.03.1"},
		},
	}
	if v := c.CurrentVersion(s); v != "2026.03.1" {
		t.Errorf("CurrentVersion = %q, want %q", v, "2026.03.1")
	}
}

func TestPlanReturnsActions(t *testing.T) {
	c := New()
	plan, err := c.Plan("", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Error("Plan should return actions for new install")
	}
}

func TestPlanCreatesTwoAccounts_DefaultUser(t *testing.T) {
	c := New()
	// Manifest without an explicit OnrampUser; the component should
	// default to "aether" (distinct from the "aether-ops" daemon).
	c.SetManifest(&bundle.Manifest{
		Components: bundle.ComponentList{
			AetherOps: &bundle.AetherOpsEntry{},
		},
	})
	plan, err := c.Plan("", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("len(Actions) = %d, want 2 (daemon + onramp)", len(plan.Actions))
	}
	// Order matters: the daemon account is created first so the
	// service account's group exists before anything else that tries
	// to chown into /var/lib/aether-ops.
	if got := plan.Actions[0].Description; got != "create daemon account aether-ops" {
		t.Errorf("Actions[0] = %q, want %q", got, "create daemon account aether-ops")
	}
	if got := plan.Actions[1].Description; got != "create onramp user aether" {
		t.Errorf("Actions[1] = %q, want %q", got, "create onramp user aether")
	}
}

func TestPlanUsesExplicitOnrampUser(t *testing.T) {
	c := New()
	c.SetManifest(&bundle.Manifest{
		Components: bundle.ComponentList{
			AetherOps: &bundle.AetherOpsEntry{OnrampUser: "ops-engineer"},
		},
	})
	plan, err := c.Plan("", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Actions[1].Description != "create onramp user ops-engineer" {
		t.Errorf("Actions[1] = %q, want the configured onramp user", plan.Actions[1].Description)
	}
}

func TestValidateUsername(t *testing.T) {
	ok := []string{"aether", "aether-ops", "user_1", "a"}
	for _, name := range ok {
		if err := validateUsername(name); err != nil {
			t.Errorf("validateUsername(%q) = %v, want nil", name, err)
		}
	}
	bad := []string{
		"",                      // empty
		"1aether",               // digit first
		"-aether",               // dash first
		"_aether",               // underscore first
		"aether;rm",             // shell metachar
		"aether ",               // whitespace
		"aether$",               // shell metachar
		strings.Repeat("a", 33), // too long
	}
	for _, name := range bad {
		if err := validateUsername(name); err == nil {
			t.Errorf("validateUsername(%q) returned nil, want error", name)
		}
	}
}
