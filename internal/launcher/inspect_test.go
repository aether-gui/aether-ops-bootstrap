package launcher

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/archive"
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// makeBundle materializes a single-manifest bundle.tar.zst from m and
// returns its path. Sidecar generation is the caller's responsibility.
func makeBundle(t *testing.T, m *bundle.Manifest) string {
	t.Helper()
	stage := t.TempDir()
	if err := bundle.Write(filepath.Join(stage, "manifest.json"), m); err != nil {
		t.Fatalf("bundle.Write: %v", err)
	}
	out := filepath.Join(t.TempDir(), "bundle.tar.zst")
	if err := archive.Archive(stage, out); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	return out
}

func TestInspectTextFullManifest(t *testing.T) {
	m := &bundle.Manifest{
		SchemaVersion: bundle.SchemaVersion,
		BundleVersion: "2026.05.27.1",
		BuildInfo: bundle.BuildInfo{
			GoVersion: "go1.26.3",
			GitSHA:    "abc1234567890",
			Builder:   "build-bundle",
			Timestamp: "2026-05-28T14:22:10Z",
		},
		Components: bundle.ComponentList{
			RKE2: &bundle.RKE2Entry{Version: "v1.33.1+rke2r1", Variants: []string{"canal"}, ImageMode: "all-in-one"},
			Helm: &bundle.HelmEntry{Version: "v3.21.0"},
			AetherOps: &bundle.AetherOpsEntry{
				Version: "v0.2.3",
				Source: &bundle.AetherOpsSource{
					Mode: "release",
					Repo: "aether-gui/aether-ops",
					Ref:  "v0.2.3",
				},
			},
			Onramp: &bundle.OnrampEntry{
				Repo:        "https://github.com/opennetworkinglab/aether-onramp.git",
				Ref:         "main",
				ResolvedSHA: "ab12cd34ef56",
				TreeSHA256:  "7e9f1234abcd",
				Patches: []bundle.PatchRecord{
					{
						Kind:      "builtin",
						Target:    "vars/main.yml",
						Applier:   "build-bundle:onrampPatches",
						Source:    "set vars/main.yml:airgapped.enabled=true",
						Timestamp: "2026-05-28T14:22:01Z",
					},
					{
						Kind:      "user",
						Target:    "vars/site.yml",
						Applier:   "build-bundle",
						Source:    "/home/me/patches/site.yml",
						Timestamp: "2026-05-28T14:22:02Z",
					},
					{
						Kind:      "post-build",
						Target:    "vars/extra.yml",
						Applier:   "patch-bundle",
						Source:    "<inline content>",
						Timestamp: "2026-05-29T09:11:00Z",
					},
				},
			},
			HelmCharts: []bundle.HelmChartsEntry{
				{Name: "sdcore", Ref: "v1.5.0", ResolvedSHA: "9988770000ff"},
			},
		},
	}
	path := makeBundle(t, m)

	buf := &bytes.Buffer{}
	if err := Inspect(InspectOpts{BundlePath: path, Out: buf}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	out := buf.String()

	mustContain := []string{
		"Version:  2026.05.27.1",
		"Built:    2026-05-28T14:22:10Z by build-bundle",
		"go1.26.3",
		"abc123456789",
		"Integrity",
		"RKE2",
		"v1.33.1+rke2r1",
		"variants: canal",
		"image_mode: all-in-one",
		"Helm",
		"v3.21.0",
		"aether-ops",
		"source: release",
		"ref: v0.2.3",
		"onramp",
		"ab12cd34ef56",
		"helm-charts/sdcore",
		"Patches (onramp)",
		"[builtin]",
		"[user]",
		"[post-build]",
		"← /home/me/patches/site.yml",
		"(inline)",
		// no manifest-claim hash recorded on this fixture.
		"(not recorded — likely patched after build)",
		// no sidecar produced.
		"(no sidecar found)",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("inspect output missing %q\n---\n%s", want, out)
		}
	}
}

func TestInspectTextBackwardCompat(t *testing.T) {
	// Pre-extension manifest: no AetherOpsSource, no Patches, no
	// BundleSHA256. Must render without panicking and show em-dash
	// placeholders for the missing pieces.
	m := &bundle.Manifest{
		SchemaVersion: bundle.SchemaVersion,
		BundleVersion: "2026.04.1",
		BuildInfo:     bundle.BuildInfo{Timestamp: "2026-04-07T00:00:00Z", Builder: "build-bundle"},
		Components: bundle.ComponentList{
			AetherOps: &bundle.AetherOpsEntry{Version: "v0.1.0"},
		},
	}
	path := makeBundle(t, m)

	buf := &bytes.Buffer{}
	if err := Inspect(InspectOpts{BundlePath: path, Out: buf}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "source: —") {
		t.Errorf("expected em-dash for missing AetherOpsSource, got:\n%s", out)
	}
	if strings.Contains(out, "Patches (onramp)") {
		t.Errorf("Patches section should not render when no patches present:\n%s", out)
	}
}

func TestInspectIntegrityMatchesSidecar(t *testing.T) {
	m := &bundle.Manifest{
		SchemaVersion: bundle.SchemaVersion,
		BundleVersion: "1.0",
		BuildInfo:     bundle.BuildInfo{Timestamp: "t", Builder: "b"},
	}
	path := makeBundle(t, m)

	// Write a sidecar that matches the actual hash.
	hash, err := hashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".sha256", []byte(hash+"  "+filepath.Base(path)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	buf := &bytes.Buffer{}
	if err := Inspect(InspectOpts{BundlePath: path, Out: buf}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "(matches sidecar)") {
		t.Errorf("expected 'matches sidecar', got:\n%s", out)
	}
}

func TestInspectIntegritySidecarMismatch(t *testing.T) {
	m := &bundle.Manifest{
		SchemaVersion: bundle.SchemaVersion,
		BundleVersion: "1.0",
		BuildInfo:     bundle.BuildInfo{Timestamp: "t", Builder: "b"},
	}
	path := makeBundle(t, m)

	// Write a deliberately wrong sidecar.
	bogus := "00" + strings.Repeat("ff", 31)
	if err := os.WriteFile(path+".sha256", []byte(bogus+"  "+filepath.Base(path)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	buf := &bytes.Buffer{}
	if err := Inspect(InspectOpts{BundlePath: path, Out: buf}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "MISMATCH sidecar=") {
		t.Errorf("expected sidecar MISMATCH, got:\n%s", out)
	}
}

func TestInspectJSONReemitsManifest(t *testing.T) {
	m := &bundle.Manifest{
		SchemaVersion: bundle.SchemaVersion,
		BundleVersion: "1.0",
		Components: bundle.ComponentList{
			AetherOps: &bundle.AetherOpsEntry{Version: "v0.2.3"},
		},
	}
	path := makeBundle(t, m)

	buf := &bytes.Buffer{}
	if err := Inspect(InspectOpts{BundlePath: path, Out: buf, JSON: true}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	var got bundle.Manifest
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if got.BundleVersion != "1.0" {
		t.Errorf("BundleVersion = %q", got.BundleVersion)
	}
	if got.Components.AetherOps == nil || got.Components.AetherOps.Version != "v0.2.3" {
		t.Errorf("AetherOps not round-tripped: %+v", got.Components.AetherOps)
	}
}

func TestInspectRejectsMissingBundle(t *testing.T) {
	err := Inspect(InspectOpts{BundlePath: filepath.Join(t.TempDir(), "missing.tar.zst"), Out: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected error for missing bundle")
	}
}

func TestInspectRequiresOutAndPath(t *testing.T) {
	if err := Inspect(InspectOpts{BundlePath: "x"}); err == nil {
		t.Error("expected error when Out is nil")
	}
	if err := Inspect(InspectOpts{Out: &bytes.Buffer{}}); err == nil {
		t.Error("expected error when BundlePath is empty")
	}
}
