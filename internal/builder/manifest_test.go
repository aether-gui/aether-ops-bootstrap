package builder

import (
	"runtime"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

func TestBuildManifest(t *testing.T) {
	spec := &bundle.Spec{
		SchemaVersion: 1,
		BundleVersion: "2026.04.1",
	}

	rke2Entry := &bundle.RKE2Entry{
		Version:   "v1.33.1+rke2r1",
		Variants:  []string{"canal"},
		ImageMode: bundle.ImageModeAllInOne,
		Artifacts: []bundle.RKE2Artifact{
			{Type: "binary", Arch: "amd64", Path: "rke2/rke2.linux-amd64.tar.gz", SHA256: "abc", Size: 100},
		},
	}

	debEntries := []bundle.DebEntry{
		{Name: "git", Version: "1:2.43.0-1", Arch: "amd64", Suite: "noble", Filename: "debs/noble/amd64/git.deb", SHA256: "xyz"},
	}

	m := BuildManifest(spec, rke2Entry, debEntries)

	if m.SchemaVersion != bundle.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", m.SchemaVersion, bundle.SchemaVersion)
	}
	if m.BundleVersion != "2026.04.1" {
		t.Errorf("BundleVersion = %q", m.BundleVersion)
	}
	if m.BuildInfo.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", m.BuildInfo.GoVersion, runtime.Version())
	}
	if m.BuildInfo.Builder != "build-bundle" {
		t.Errorf("Builder = %q", m.BuildInfo.Builder)
	}
	if m.BuildInfo.Timestamp == "" {
		t.Error("Timestamp is empty")
	}
	if m.Components.RKE2 == nil {
		t.Fatal("RKE2 is nil")
	}
	if m.Components.RKE2.Version != "v1.33.1+rke2r1" {
		t.Errorf("RKE2.Version = %q", m.Components.RKE2.Version)
	}
	if m.BundleSHA256 != "" {
		t.Errorf("BundleSHA256 should be empty, got %q", m.BundleSHA256)
	}
	if len(m.Components.Debs) != 1 {
		t.Fatalf("len(Debs) = %d, want 1", len(m.Components.Debs))
	}
	if m.Components.Debs[0].Name != "git" {
		t.Errorf("Debs[0].Name = %q", m.Components.Debs[0].Name)
	}
}

func TestBuildManifestNilEntries(t *testing.T) {
	spec := &bundle.Spec{
		SchemaVersion: 1,
		BundleVersion: "2026.04.1",
	}

	m := BuildManifest(spec, nil, nil)

	if m.Components.RKE2 != nil {
		t.Error("RKE2 should be nil when no entry provided")
	}
	if len(m.Components.Debs) != 0 {
		t.Errorf("Debs should be empty, got %d", len(m.Components.Debs))
	}
}
