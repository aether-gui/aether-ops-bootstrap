package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

func TestBuildLockfile(t *testing.T) {
	entries := []bundle.DebEntry{
		{Name: "git", Version: "1:2.43.0-1", Arch: "amd64", Suite: "noble", SHA256: "aaa"},
		{Name: "perl", Version: "5.38.2-3", Arch: "amd64", Suite: "noble", SHA256: "bbb"},
	}

	lf := BuildLockfile(entries)

	if lf.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d", lf.SchemaVersion)
	}

	noble, ok := lf.Debs["noble/amd64"]
	if !ok {
		t.Fatal("missing noble/amd64 section")
	}
	if noble["git"].Version != "1:2.43.0-1" {
		t.Errorf("git version = %q", noble["git"].Version)
	}
	if noble["perl"].SHA256 != "bbb" {
		t.Errorf("perl sha256 = %q", noble["perl"].SHA256)
	}
}

func TestLockfileRoundTrip(t *testing.T) {
	lf := &Lockfile{
		SchemaVersion: 1,
		Debs: map[string]map[string]LockEntry{
			"noble/amd64": {
				"git": {Version: "1:2.43.0-1", SHA256: "aaa"},
			},
		},
	}

	path := filepath.Join(t.TempDir(), "test.lock.json")
	if err := WriteLockfile(path, lf); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	got, err := ReadLockfile(path)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got == nil {
		t.Fatal("ReadLockfile returned nil")
	}

	if got.Debs["noble/amd64"]["git"].Version != "1:2.43.0-1" {
		t.Errorf("round-trip version = %q", got.Debs["noble/amd64"]["git"].Version)
	}
}

func TestReadLockfileEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.lock.json")
	if err := WriteLockfile(path, &Lockfile{SchemaVersion: 1, Debs: map[string]map[string]LockEntry{}}); err != nil {
		t.Fatal(err)
	}
	// Overwrite with just "{}"
	if err := writeTestFile(path, "{}"); err != nil {
		t.Fatal(err)
	}

	lf, err := ReadLockfile(path)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if lf != nil {
		t.Error("expected nil for empty lockfile")
	}
}

func TestReadLockfileMissing(t *testing.T) {
	lf, err := ReadLockfile("/nonexistent/path.lock.json")
	if err != nil {
		t.Fatalf("ReadLockfile on missing file should not error: %v", err)
	}
	if lf != nil {
		t.Error("expected nil for missing lockfile")
	}
}

func TestVerifyLockfileMatch(t *testing.T) {
	lf := &Lockfile{
		SchemaVersion: 1,
		Debs: map[string]map[string]LockEntry{
			"noble/amd64": {
				"git": {Version: "1:2.43.0-1", SHA256: "aaa"},
			},
		},
	}

	if err := VerifyLockfile(lf, lf); err != nil {
		t.Fatalf("identical lockfiles should match: %v", err)
	}
}

func TestVerifyLockfileVersionDrift(t *testing.T) {
	existing := &Lockfile{
		SchemaVersion: 1,
		Debs: map[string]map[string]LockEntry{
			"noble/amd64": {
				"git": {Version: "1:2.43.0-1", SHA256: "aaa"},
			},
		},
	}
	current := &Lockfile{
		SchemaVersion: 1,
		Debs: map[string]map[string]LockEntry{
			"noble/amd64": {
				"git": {Version: "1:2.44.0-1", SHA256: "bbb"},
			},
		},
	}

	err := VerifyLockfile(existing, current)
	if err == nil {
		t.Fatal("should detect version drift")
	}
}

func TestVerifyLockfileHashDrift(t *testing.T) {
	existing := &Lockfile{
		SchemaVersion: 1,
		Debs: map[string]map[string]LockEntry{
			"noble/amd64": {
				"git": {Version: "1:2.43.0-1", SHA256: "aaa"},
			},
		},
	}
	current := &Lockfile{
		SchemaVersion: 1,
		Debs: map[string]map[string]LockEntry{
			"noble/amd64": {
				"git": {Version: "1:2.43.0-1", SHA256: "bbb"},
			},
		},
	}

	err := VerifyLockfile(existing, current)
	if err == nil {
		t.Fatal("should detect hash drift")
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
