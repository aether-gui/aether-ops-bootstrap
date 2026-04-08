package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	m := &Manifest{
		SchemaVersion: SchemaVersion,
		BundleVersion: "2026.04.1",
		BundleSHA256:  "abc123",
		BuildInfo: BuildInfo{
			GoVersion: "go1.22.2",
			GitSHA:    "deadbeef",
			Builder:   "ci",
			Timestamp: "2026-04-07T00:00:00Z",
		},
		Components: ComponentList{
			Debs: []DebEntry{
				{Name: "git", Version: "1:2.43.0-1", Arch: "amd64", Suite: "noble", Filename: "debs/noble/amd64/git.deb", SHA256: "def456"},
			},
			RKE2: &RKE2Entry{
				Version:   "v1.33.1+rke2r1",
				Variants:  []string{"canal"},
				ImageMode: "all-in-one",
				Artifacts: []RKE2Artifact{
					{Type: "binary", Arch: "amd64", Path: "rke2/rke2.linux-amd64.tar.gz", SHA256: "aaa", Size: 54321000},
					{Type: "images", Arch: "amd64", Path: "rke2/rke2-images.linux-amd64.tar.zst", SHA256: "bbb", Size: 987654000},
					{Type: "checksum", Arch: "amd64", Path: "rke2/sha256sum-amd64.txt", SHA256: "ccc", Size: 1234},
				},
			},
			AetherOps: &AetherOpsEntry{
				Version: "1.4.0",
				Files: []BundleFile{
					{Path: "aether-ops/aether-ops", SHA256: "bbb", Size: 12345000},
				},
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	if err := Write(path, m); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.SchemaVersion != m.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, m.SchemaVersion)
	}
	if got.BundleVersion != m.BundleVersion {
		t.Errorf("BundleVersion = %q, want %q", got.BundleVersion, m.BundleVersion)
	}
	if got.BundleSHA256 != m.BundleSHA256 {
		t.Errorf("BundleSHA256 = %q, want %q", got.BundleSHA256, m.BundleSHA256)
	}
	if got.BuildInfo.GoVersion != m.BuildInfo.GoVersion {
		t.Errorf("BuildInfo.GoVersion = %q, want %q", got.BuildInfo.GoVersion, m.BuildInfo.GoVersion)
	}
	if len(got.Components.Debs) != 1 {
		t.Fatalf("len(Debs) = %d, want 1", len(got.Components.Debs))
	}
	if got.Components.Debs[0].Name != "git" {
		t.Errorf("Debs[0].Name = %q, want %q", got.Components.Debs[0].Name, "git")
	}
	if got.Components.Debs[0].Suite != "noble" {
		t.Errorf("Debs[0].Suite = %q, want %q", got.Components.Debs[0].Suite, "noble")
	}
	if got.Components.RKE2 == nil {
		t.Fatal("RKE2 is nil, want non-nil")
	}
	if got.Components.RKE2.Version != "v1.33.1+rke2r1" {
		t.Errorf("RKE2.Version = %q, want %q", got.Components.RKE2.Version, "v1.33.1+rke2r1")
	}
	if got.Components.RKE2.ImageMode != "all-in-one" {
		t.Errorf("RKE2.ImageMode = %q, want %q", got.Components.RKE2.ImageMode, "all-in-one")
	}
	if len(got.Components.RKE2.Artifacts) != 3 {
		t.Fatalf("len(RKE2.Artifacts) = %d, want 3", len(got.Components.RKE2.Artifacts))
	}
	if got.Components.RKE2.Artifacts[0].Type != "binary" {
		t.Errorf("RKE2.Artifacts[0].Type = %q, want %q", got.Components.RKE2.Artifacts[0].Type, "binary")
	}
	if got.Components.RKE2.Artifacts[1].Type != "images" {
		t.Errorf("RKE2.Artifacts[1].Type = %q, want %q", got.Components.RKE2.Artifacts[1].Type, "images")
	}
	if got.Components.AetherOps == nil {
		t.Fatal("AetherOps is nil, want non-nil")
	}
	if got.Components.AetherOps.Version != "1.4.0" {
		t.Errorf("AetherOps.Version = %q, want %q", got.Components.AetherOps.Version, "1.4.0")
	}
}

func TestReadMissingFile(t *testing.T) {
	_, err := Read("/nonexistent/path/manifest.json")
	if err == nil {
		t.Fatal("Read with missing file should return error")
	}
}

func TestReadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Read(path)
	if err == nil {
		t.Fatal("Read with invalid JSON should return error")
	}
}

func TestWriteAndReadEmptyManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")

	m := &Manifest{SchemaVersion: SchemaVersion}

	if err := Write(path, m); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, SchemaVersion)
	}
	if got.Components.RKE2 != nil {
		t.Errorf("RKE2 should be nil for empty manifest")
	}
}
