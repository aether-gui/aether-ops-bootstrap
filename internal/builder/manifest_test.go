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

	aetherOpsEntry := &bundle.AetherOpsEntry{
		Version: "v1.0.0",
		Files: []bundle.BundleFile{
			{Path: "aether-ops/aether-ops", SHA256: "def", Size: 5000},
		},
	}

	m := BuildManifest(spec, rke2Entry, aetherOpsEntry)

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
	if m.Components.AetherOps == nil {
		t.Fatal("AetherOps is nil")
	}
	if m.Components.AetherOps.Version != "v1.0.0" {
		t.Errorf("AetherOps.Version = %q", m.Components.AetherOps.Version)
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
	if m.Components.AetherOps != nil {
		t.Error("AetherOps should be nil when no entry provided")
	}
}
