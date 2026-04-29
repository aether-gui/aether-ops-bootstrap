package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/builder"
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// fakeBundle builds a tiny bundle.tar.zst whose only purpose is to
// exercise the patch-bundle round-trip end-to-end. The "onramp" tree
// has three files; the manifest declares an OnrampEntry pointing at
// it. Returns the path to the .tar.zst.
func fakeBundle(t *testing.T, onrampFiles map[string]string) string {
	t.Helper()
	stage := t.TempDir()

	// Stage: onramp/aether-onramp/<files>
	relRoot := filepath.Join("onramp", "aether-onramp")
	for rel, content := range onrampFiles {
		full := filepath.Join(stage, relRoot, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := builder.HashTree(filepath.Join(stage, relRoot), relRoot)
	if err != nil {
		t.Fatal(err)
	}

	m := &bundle.Manifest{
		SchemaVersion: bundle.SchemaVersion,
		BundleVersion: "test-1",
		Components: bundle.ComponentList{
			Onramp: &bundle.OnrampEntry{
				Repo:        "file:///fake",
				ResolvedSHA: "fakesha",
				Path:        relRoot,
				Files:       files,
				TreeSHA256:  bundle.ComputeTreeSHA256(files),
			},
		},
	}
	if err := bundle.Write(filepath.Join(stage, "manifest.json"), m); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "bundle.tar.zst")
	if err := builder.Archive(stage, out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPatchBundleEndToEnd(t *testing.T) {
	bundlePath := fakeBundle(t, map[string]string{
		"ocudu/roles/uEsimulator/templates/ue_zmq.conf": "upstream ue\n",
		"ocudu/roles/gNB/templates/gnb_zmq.yaml":        "upstream gnb\n",
		"untouched.txt":                                 "leave me alone\n",
	})

	// Local file backing one of the replacements.
	localFile := filepath.Join(t.TempDir(), "ue_zmq.conf")
	if err := os.WriteFile(localFile, []byte("operator ue\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(t.TempDir(), "patched.tar.zst")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := run([]string{
		"--in", bundlePath,
		"--out", outPath,
		"--replace", "ocudu/roles/uEsimulator/templates/ue_zmq.conf=" + localFile,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr=%s", err, stderr.String())
	}

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if _, err := os.Stat(outPath + ".sha256"); err != nil {
		t.Fatalf("checksum sidecar missing: %v", err)
	}

	// Round-trip the patched bundle and inspect its manifest + content.
	extract := t.TempDir()
	if err := builder.Unarchive(outPath, extract); err != nil {
		t.Fatalf("unarchive output: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(extract, "onramp", "aether-onramp",
		"ocudu", "roles", "uEsimulator", "templates", "ue_zmq.conf"))
	if err != nil {
		t.Fatalf("reading patched file: %v", err)
	}
	if string(got) != "operator ue\n" {
		t.Errorf("patched file content = %q, want %q", got, "operator ue\n")
	}

	// Untouched file must still be there with original content.
	untouched, err := os.ReadFile(filepath.Join(extract, "onramp", "aether-onramp", "untouched.txt"))
	if err != nil {
		t.Fatalf("reading untouched file: %v", err)
	}
	if string(untouched) != "leave me alone\n" {
		t.Errorf("untouched file changed: %q", untouched)
	}

	// Manifest hashes must match patched bytes.
	m, err := bundle.Read(filepath.Join(extract, "manifest.json"))
	if err != nil {
		t.Fatalf("reading patched manifest: %v", err)
	}
	wantHash := hashOf("operator ue\n")
	var foundPatched, foundUntouched bool
	for _, f := range m.Components.Onramp.Files {
		if strings.HasSuffix(f.Path, "ue_zmq.conf") {
			foundPatched = true
			if f.SHA256 != wantHash {
				t.Errorf("patched file manifest hash = %s, want %s", f.SHA256, wantHash)
			}
		}
		if strings.HasSuffix(f.Path, "untouched.txt") {
			foundUntouched = true
			if f.SHA256 != hashOf("leave me alone\n") {
				t.Errorf("untouched manifest hash drifted: %s", f.SHA256)
			}
		}
	}
	if !foundPatched {
		t.Error("patched file not in manifest")
	}
	if !foundUntouched {
		t.Error("untouched file dropped from manifest")
	}

	// TreeSHA256 must reflect the new content.
	if m.Components.Onramp.TreeSHA256 == "" {
		t.Error("TreeSHA256 missing on patched manifest")
	}
	if m.Components.Onramp.TreeSHA256 == bundleTreeSHA(t, bundlePath) {
		t.Error("TreeSHA256 unchanged after patch — composeVersion would not detect re-extract")
	}
}

func TestPatchBundleRejectsInPlaceOverwrite(t *testing.T) {
	bundlePath := fakeBundle(t, map[string]string{"x.conf": "x\n"})
	err := run([]string{
		"--in", bundlePath,
		"--out", bundlePath,
		"--replace", "x.conf=" + bundlePath,
	}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error rejecting in-place overwrite")
	}
	if !strings.Contains(err.Error(), "in place") {
		t.Errorf("error should mention in-place; got %v", err)
	}
}

func TestPatchBundleErrorsWhenTargetMissing(t *testing.T) {
	bundlePath := fakeBundle(t, map[string]string{"x.conf": "x\n"})
	localFile := filepath.Join(t.TempDir(), "y.conf")
	if err := os.WriteFile(localFile, []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "patched.tar.zst")

	err := run([]string{
		"--in", bundlePath,
		"--out", outPath,
		"--replace", "no-such-file=" + localFile,
	}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error when target file is absent from bundle")
	}
	// Output must not exist on failure (atomic write semantics).
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Errorf("output bundle should not exist after failure")
	}
	if _, statErr := os.Stat(outPath + ".sha256"); !os.IsNotExist(statErr) {
		t.Errorf("checksum sidecar should not exist after failure")
	}
}

func TestPatchBundleRejectsBothInputModes(t *testing.T) {
	bundlePath := fakeBundle(t, map[string]string{"x.conf": "x\n"})
	out := filepath.Join(t.TempDir(), "patched.tar.zst")
	patches := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(patches, []byte("schema_version: 1\npatches: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run([]string{
		"--in", bundlePath, "--out", out,
		"--replace", "x.conf=/dev/null",
		"--patches", patches,
	}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually-exclusive error, got %v", err)
	}
}

func TestPatchBundlePatchesYAMLFile(t *testing.T) {
	bundlePath := fakeBundle(t, map[string]string{"x.conf": "old\n"})

	patchesDir := t.TempDir()
	src := filepath.Join(patchesDir, "x.conf")
	if err := os.WriteFile(src, []byte("inline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patchesYAML := filepath.Join(patchesDir, "patches.yaml")
	yaml := "" +
		"schema_version: 1\n" +
		"patches:\n" +
		"  - target: x.conf\n" +
		"    source: ./x.conf\n"
	if err := os.WriteFile(patchesYAML, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(t.TempDir(), "patched.tar.zst")
	if err := run([]string{
		"--in", bundlePath, "--out", outPath,
		"--patches", patchesYAML,
	}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	extract := t.TempDir()
	if err := builder.Unarchive(outPath, extract); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(extract, "onramp", "aether-onramp", "x.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "inline\n" {
		t.Errorf("content = %q, want %q", got, "inline\n")
	}
}

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func bundleTreeSHA(t *testing.T, bundlePath string) string {
	t.Helper()
	dir := t.TempDir()
	if err := builder.Unarchive(bundlePath, dir); err != nil {
		t.Fatal(err)
	}
	m, err := bundle.Read(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m.Components.Onramp.TreeSHA256
}
