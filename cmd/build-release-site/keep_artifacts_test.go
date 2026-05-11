package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMaterializeArtifact_KeepExisting verifies that
// --keep-existing-artifacts reads the SHA256 from a pre-existing
// sidecar instead of copying art.Source and rehashing. The artifact
// file itself must be left untouched.
func TestMaterializeArtifact_KeepExisting(t *testing.T) {
	out := t.TempDir()

	const (
		path    = "2026.05.11.1"
		fname   = "aether-ops-bootstrap"
		hash    = "deadbeefcafef00d00000000000000000000000000000000000000000000abcd"
		payload = "pretend-this-is-the-already-published-launcher-binary"
	)
	stage := filepath.Join(out, "bootstrap", path)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, fname), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(stage, fname+".sha256")
	if err := os.WriteFile(sidecar, []byte(hash+"  "+fname+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	art := artifactConfig{
		Version:  "2026.05.11.1",
		Path:     path,
		Filename: fname,
		// Source intentionally points at a path that doesn't exist —
		// keepArtifacts mode must not try to read it.
		Source: "/nonexistent/source",
	}

	ra, pa, err := materializeArtifact(
		"" /*metadataDir*/, out, "/aether-ops-bootstrap", "bootstrap", art,
		false /*external*/, nil /*kinds*/, true, /*keepArtifacts*/
	)
	if err != nil {
		t.Fatalf("materializeArtifact: %v", err)
	}
	if ra.SHA256 != hash {
		t.Errorf("rendered SHA = %q, want %q", ra.SHA256, hash)
	}
	if pa.SHA256 != hash {
		t.Errorf("public SHA = %q, want %q", pa.SHA256, hash)
	}

	// Original artifact file must still be present and unmodified.
	got, err := os.ReadFile(filepath.Join(stage, fname))
	if err != nil {
		t.Fatalf("artifact file gone: %v", err)
	}
	if string(got) != payload {
		t.Errorf("artifact body changed; got %q, want %q", got, payload)
	}
}

// TestMaterializeArtifact_KeepExisting_MissingSidecar verifies that
// keepArtifacts errors loudly with a useful message when the
// pre-existing sidecar isn't on disk — failing fast is the correct
// behavior for the republish workflow.
func TestMaterializeArtifact_KeepExisting_MissingSidecar(t *testing.T) {
	out := t.TempDir()

	art := artifactConfig{
		Version:  "2026.05.11.1",
		Path:     "2026.05.11.1",
		Filename: "aether-ops-bootstrap",
		Source:   "../dist/aether-ops-bootstrap",
	}

	_, _, err := materializeArtifact(
		"" /*metadataDir*/, out, "/aether-ops-bootstrap", "bootstrap", art,
		false /*external*/, nil /*kinds*/, true, /*keepArtifacts*/
	)
	if err == nil {
		t.Fatal("expected error when sidecar is missing, got nil")
	}
	if !strings.Contains(err.Error(), "keep-existing-artifacts") {
		t.Errorf("error message %q does not mention --keep-existing-artifacts", err.Error())
	}
	if !strings.Contains(err.Error(), "aether-ops-bootstrap.sha256") {
		t.Errorf("error message %q does not name the missing sidecar", err.Error())
	}
}
