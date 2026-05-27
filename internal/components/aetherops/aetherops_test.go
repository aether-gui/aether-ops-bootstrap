package aetherops

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
	c := New("", nil)
	if c.Name() != "aether_ops" {
		t.Errorf("Name() = %q, want %q", c.Name(), "aether_ops")
	}
}

func TestDesiredVersion(t *testing.T) {
	c := New("", nil)
	m := &bundle.Manifest{
		Components: bundle.ComponentList{
			AetherOps: &bundle.AetherOpsEntry{Version: "1.4.0"},
		},
	}
	if v := c.DesiredVersion(m); v != "1.4.0" {
		t.Errorf("DesiredVersion = %q, want %q", v, "1.4.0")
	}
}

func TestDesiredVersionNilAetherOps(t *testing.T) {
	c := New("", nil)
	m := &bundle.Manifest{}
	if v := c.DesiredVersion(m); v != "" {
		t.Errorf("DesiredVersion with nil AetherOps = %q, want empty", v)
	}
}

func TestCurrentVersion(t *testing.T) {
	c := New("", nil)
	s := &state.State{
		Components: map[string]state.ComponentState{
			"aether_ops": {Version: "1.3.0"},
		},
	}
	if v := c.CurrentVersion(s); v != "1.3.0" {
		t.Errorf("CurrentVersion = %q, want %q", v, "1.3.0")
	}
}

func TestPlanNoOpWhenNilManifest(t *testing.T) {
	c := New("", nil)
	plan, err := c.Plan("", "1.4.0")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.NoOp {
		t.Error("Plan should be NoOp when manifest is nil")
	}
}

func TestPlanIncludesOnrampPasswordWrite(t *testing.T) {
	c := New("", &bundle.Manifest{
		Components: bundle.ComponentList{
			AetherOps: &bundle.AetherOpsEntry{Version: "1.4.0"},
		},
	})
	plan, err := c.Plan("", "1.4.0")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.NoOp {
		t.Fatal("Plan should not be NoOp")
	}
	found := false
	for _, a := range plan.Actions {
		if a.Description == "write onramp password file" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Plan missing 'write onramp password file' action; got: %v", actionDescriptions(plan))
	}
}

func TestWriteOnrampPasswordFile_WritesContentAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onramp-password")
	if err := writeOnrampPasswordFileAs(path, "s3cret", -1, -1); err != nil {
		t.Fatalf("writeOnrampPasswordFileAs: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "s3cret" {
		t.Errorf("file content = %q, want %q (no trailing newline)", got, "s3cret")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0640 {
		t.Errorf("mode = %o, want 0640", mode)
	}
}

func TestWriteOnrampPasswordFile_RejectsInvalidPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onramp-password")
	// Newline is rejected by ValidateOnrampPassword because it would
	// corrupt the hosts.ini line the daemon stamps it into.
	err := writeOnrampPasswordFileAs(path, "bad\npass", -1, -1)
	if err == nil {
		t.Fatal("writeOnrampPasswordFileAs should reject password with newline")
	}
}

func TestWriteOnrampPasswordFile_TightensModeOnRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onramp-password")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := writeOnrampPasswordFileAs(path, "new", -1, -1); err != nil {
		t.Fatalf("writeOnrampPasswordFileAs: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0640 {
		t.Errorf("mode after rewrite = %o, want 0640 (must tighten from 0644 seed)", mode)
	}
}

func actionDescriptions(plan components.Plan) []string {
	out := make([]string, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		out = append(out, a.Description)
	}
	return out
}
