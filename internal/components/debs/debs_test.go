package debs

import (
	"slices"
	"strings"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

func TestNonInteractiveDpkgEnv(t *testing.T) {
	env := nonInteractiveDpkgEnv()

	// Required variables that keep debconf silent. If any are dropped
	// or renamed, the install hangs on maintainer-script prompts (see
	// function comment for the full story).
	required := []string{
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBIAN_PRIORITY=critical",
	}
	for _, want := range required {
		if !slices.Contains(env, want) {
			t.Errorf("env missing %q; full env:\n%s", want, strings.Join(env, "\n"))
		}
	}

	// Regression guard: os.Environ() is appended, not replaced. If a
	// future refactor drops the parent env, operator-supplied proxies
	// and locale settings would silently disappear. PATH is set in
	// practice on every host the installer runs on, so its presence is
	// a reasonable proxy for "we inherited something".
	inheritedPath := slices.ContainsFunc(env, func(e string) bool {
		return strings.HasPrefix(e, "PATH=")
	})
	if !inheritedPath {
		t.Error("env did not inherit PATH from os.Environ(); operator-supplied environment was dropped")
	}
}

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
