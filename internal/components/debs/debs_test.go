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
	repoPath := "/var/lib/aether-ops/apt-repo"
	suite := "noble"

	if err := writeSourcesList(path, repoPath, suite); err != nil {
		t.Fatalf("writeSourcesList: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	body := string(got)
	wantLine := "deb [trusted=yes] file:///var/lib/aether-ops/apt-repo noble main\n"
	if !strings.Contains(body, wantLine) {
		t.Errorf("sources.list missing %q\n--- got ---\n%s", wantLine, body)
	}
	if !strings.Contains(body, "# Aether-ops bundle local apt repository.") {
		t.Errorf("sources.list missing operator-facing header comment\n--- got ---\n%s", body)
	}
}

func TestWriteSourcesListCreatesParent(t *testing.T) {
	dir := t.TempDir()
	// Nested path that does not yet exist — writeSourcesList should
	// MkdirAll, since /etc/apt/sources.list.d/ exists on real hosts
	// but tempdirs in tests start empty.
	path := filepath.Join(dir, "etc", "apt", "sources.list.d", "aether-bundle.list")

	if err := writeSourcesList(path, "/var/lib/aether-ops/apt-repo", "noble"); err != nil {
		t.Fatalf("writeSourcesList: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sources.list missing after write: %v", err)
	}
}

func TestStageRepo_SameFilesystemRename(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "extract", "apt-repo")
	dst := filepath.Join(root, "var", "apt-repo")

	// Populate src with a small fixture.
	for _, p := range []string{
		"dists/noble/Release",
		"dists/noble/main/binary-amd64/Packages",
		"pool/noble/amd64/foo.deb",
	} {
		full := filepath.Join(src, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(p), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := stageRepo(src, dst); err != nil {
		t.Fatalf("stageRepo: %v", err)
	}

	// Files are now under dst; src is gone (rename) or empty (copy fallback).
	for _, p := range []string{
		"dists/noble/Release",
		"dists/noble/main/binary-amd64/Packages",
		"pool/noble/amd64/foo.deb",
	} {
		full := filepath.Join(dst, p)
		got, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("missing %s after stage: %v", full, err)
		}
		if string(got) != p {
			t.Errorf("staged %s contents = %q, want %q", full, got, p)
		}
	}
}

func TestStageRepo_ReplacesExistingDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "extract", "apt-repo")
	dst := filepath.Join(root, "var", "apt-repo")

	// Pre-populate dst with stale content from a prior install.
	stale := filepath.Join(dst, "stale", "old.deb")
	if err := os.MkdirAll(filepath.Dir(stale), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("from-old-bundle"), 0644); err != nil {
		t.Fatal(err)
	}

	// Fresh src.
	fresh := filepath.Join(src, "pool", "noble", "amd64", "new.deb")
	if err := os.MkdirAll(filepath.Dir(fresh), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("from-new-bundle"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := stageRepo(src, dst); err != nil {
		t.Fatalf("stageRepo: %v", err)
	}

	// Stale file must be gone.
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file still present after stageRepo; err=%v", err)
	}
	// Fresh file is in place.
	got, err := os.ReadFile(filepath.Join(dst, "pool", "noble", "amd64", "new.deb"))
	if err != nil || string(got) != "from-new-bundle" {
		t.Errorf("fresh file missing or wrong: contents=%q err=%v", got, err)
	}
}

func TestStageRepo_FailsWhenSrcMissing(t *testing.T) {
	root := t.TempDir()
	err := stageRepo(filepath.Join(root, "nope"), filepath.Join(root, "dst"))
	if err == nil || !strings.Contains(err.Error(), "locating staged apt-repo") {
		t.Fatalf("expected missing-src error, got %v", err)
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
	if len(plan.Actions) != 4 {
		t.Fatalf("expected 4 actions (stage repo, write sources.list, apt update, apt install); got %d", len(plan.Actions))
	}

	descriptions := []string{
		plan.Actions[0].Description,
		plan.Actions[1].Description,
		plan.Actions[2].Description,
		plan.Actions[3].Description,
	}
	for i, want := range []string{
		"stage apt-repo",
		"/etc/apt/sources.list.d/aether-bundle.list",
		"apt-get update",
		"apt-get install",
	} {
		if !strings.Contains(descriptions[i], want) {
			t.Errorf("action %d description %q does not contain %q", i, descriptions[i], want)
		}
	}
	// Top-level count must surface in the install action so operator
	// log output explains what apt is being asked to install.
	if !strings.Contains(plan.Actions[3].Description, "4 top-level packages") {
		t.Errorf("install action description should mention 4 top-level packages, got %q", plan.Actions[3].Description)
	}
}
