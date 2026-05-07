package debs

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

func TestNoninteractiveAptEnv(t *testing.T) {
	env := noninteractiveAptEnv()

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

func TestWriteSourcesList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.list")
	repoPath := "/var/lib/aether-ops/extract/apt-repo"
	suite := "noble"

	if err := writeSourcesList(path, repoPath, suite); err != nil {
		t.Fatalf("writeSourcesList: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "deb [trusted=yes] file:///var/lib/aether-ops/extract/apt-repo noble main\n"
	if string(got) != want {
		t.Errorf("sources.list = %q, want %q", string(got), want)
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
			AptRepo: &bundle.AptRepoEntry{
				Path:      "apt-repo",
				Codenames: []string{"noble"},
				TopLevel:  []string{"git"},
			},
		},
	}
	if v := c.DesiredVersion(m); v != "2026.04.1" {
		t.Errorf("DesiredVersion = %q, want %q", v, "2026.04.1")
	}
}

func TestDesiredVersionEmptyWhenAptRepoMissing(t *testing.T) {
	c := New("", nil)
	m := &bundle.Manifest{BundleVersion: "2026.04.1"}
	if v := c.DesiredVersion(m); v != "" {
		t.Errorf("DesiredVersion with no AptRepo = %q, want empty", v)
	}
}

func TestDesiredVersionEmptyWhenTopLevelEmpty(t *testing.T) {
	c := New("", nil)
	m := &bundle.Manifest{
		BundleVersion: "2026.04.1",
		Components: bundle.ComponentList{
			AptRepo: &bundle.AptRepoEntry{Path: "apt-repo"}, // no TopLevel
		},
	}
	if v := c.DesiredVersion(m); v != "" {
		t.Errorf("DesiredVersion with empty TopLevel = %q, want empty", v)
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
	m := &bundle.Manifest{
		BundleVersion: "2026.04.1",
		Components: bundle.ComponentList{
			AptRepo: &bundle.AptRepoEntry{
				Path:      "apt-repo",
				Codenames: []string{"noble"},
				TopLevel:  []string{"git"},
			},
		},
	}
	c := New("", m)
	plan, err := c.Plan("2026.04.1", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan should be NoOp when current == desired")
	}
}

func TestPlanNoOpWhenAptRepoMissing(t *testing.T) {
	m := &bundle.Manifest{BundleVersion: "2026.04.1"}
	c := New("", m)
	plan, err := c.Plan("", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan should be NoOp when manifest has no AptRepo")
	}
}

func TestPlanProducesAptActions(t *testing.T) {
	m := &bundle.Manifest{
		BundleVersion: "2026.04.1",
		Components: bundle.ComponentList{
			AptRepo: &bundle.AptRepoEntry{
				Path:      "apt-repo",
				Codenames: []string{"noble"},
				TopLevel:  []string{"ansible", "git", "ssh", "iptables-persistent"},
			},
		},
	}
	c := New("/tmp/extract", m)
	c.SetSuite("noble")

	plan, err := c.Plan("", "2026.04.1")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.NoOp {
		t.Fatal("Plan should not be NoOp when AptRepo has top-level packages")
	}
	if len(plan.Actions) != 3 {
		t.Fatalf("expected 3 actions (write sources.list, apt update, apt install); got %d", len(plan.Actions))
	}

	descriptions := []string{
		plan.Actions[0].Description,
		plan.Actions[1].Description,
		plan.Actions[2].Description,
	}
	for i, want := range []string{"write bundle sources.list", "apt-get update", "apt-get install"} {
		if !strings.Contains(descriptions[i], want) {
			t.Errorf("action %d description %q does not contain %q", i, descriptions[i], want)
		}
	}
	// Top-level count must surface in the install action so operator
	// log output explains what apt is being asked to install.
	if !strings.Contains(plan.Actions[2].Description, "4 top-level packages") {
		t.Errorf("install action description should mention 4 top-level packages, got %q", plan.Actions[2].Description)
	}
}
