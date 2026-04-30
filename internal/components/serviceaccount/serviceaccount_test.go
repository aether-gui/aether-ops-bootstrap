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

func TestPlanCreatesAccountsAndDaemonSudoers_DefaultUser(t *testing.T) {
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
	if len(plan.Actions) != 3 {
		t.Fatalf("len(Actions) = %d, want 3 (daemon + daemon sudoers + onramp)", len(plan.Actions))
	}
	// Order matters: the daemon account exists before the sudoers
	// dropin grants it sudo, and both precede the onramp user so a
	// failure in the daemon-side setup short-circuits before any
	// interactive account is provisioned.
	want := []string{
		"create daemon account aether-ops",
		"install aether-ops sudoers dropin",
		"create onramp user aether",
	}
	for i, w := range want {
		if got := plan.Actions[i].Description; got != w {
			t.Errorf("Actions[%d] = %q, want %q", i, got, w)
		}
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
	if plan.Actions[2].Description != "create onramp user ops-engineer" {
		t.Errorf("Actions[2] = %q, want the configured onramp user", plan.Actions[2].Description)
	}
}

// TestDaemonSudoersConstants pins the dropin path and content shape so a
// rename of daemonAccount can't silently desync them. The launcher
// installs whatever these constants render to, so the test doubles as a
// guard against typos in the sudoers grant itself.
func TestDaemonSudoersConstants(t *testing.T) {
	if want := "/etc/sudoers.d/" + daemonAccount; daemonSudoersPath != want {
		t.Errorf("daemonSudoersPath = %q, want %q", daemonSudoersPath, want)
	}
	if !strings.HasPrefix(daemonSudoersContent, daemonAccount+" ") {
		t.Errorf("daemonSudoersContent = %q, want prefix %q", daemonSudoersContent, daemonAccount+" ")
	}
	if !strings.Contains(daemonSudoersContent, "NOPASSWD: ALL") {
		t.Errorf("daemonSudoersContent = %q, missing NOPASSWD: ALL grant", daemonSudoersContent)
	}
	if !strings.HasSuffix(daemonSudoersContent, "\n") {
		t.Errorf("daemonSudoersContent must end with newline; got %q", daemonSudoersContent)
	}
}

func TestIsLockedShell(t *testing.T) {
	locked := []string{"/usr/sbin/nologin", "/sbin/nologin", "/bin/false", "/usr/bin/false"}
	for _, s := range locked {
		if !isLockedShell(s) {
			t.Errorf("isLockedShell(%q) = false, want true", s)
		}
	}
	loginCapable := []string{"/bin/bash", "/bin/sh", "/usr/bin/zsh", "", "/opt/homebrew/bin/fish"}
	for _, s := range loginCapable {
		if isLockedShell(s) {
			t.Errorf("isLockedShell(%q) = true, want false", s)
		}
	}
}
