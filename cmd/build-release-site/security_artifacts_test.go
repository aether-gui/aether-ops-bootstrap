package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMaterializeArtifact_WithSecurityArtifacts walks the full
// materialise path for one artifact that carries an SBOM + Grype +
// VEX sidecar. Each entry must end up:
//   - copied into <out>/<dir>/<path>/ under its declared filename
//   - hashed (computed sha must match) and stamped onto the
//     rendered/public views
//   - accompanied by a <name>.sha256 sidecar so --keep-existing-artifacts
//     can re-derive the hash on a republish.
func TestMaterializeArtifact_WithSecurityArtifacts(t *testing.T) {
	srcDir := t.TempDir()
	out := t.TempDir()

	writeFile := func(name, body string) string {
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		return p
	}
	primary := "pretend-bootstrap-binary"
	sbom := `{"spdxVersion":"SPDX-2.3"}`
	grype := `{"matches":[]}`
	vex := `{"@context":"https://openvex.dev/ns/v0.2.0","statements":[]}`
	writeFile("aether-ops-bootstrap", primary)
	writeFile("sbom-bootstrap.spdx.json", sbom)
	writeFile("grype-bootstrap.json", grype)
	writeFile("openvex.json", vex)

	art := artifactConfig{
		Version:  "2026.05.11.1",
		Path:     "2026.05.11.1",
		Filename: "aether-ops-bootstrap",
		Source:   "aether-ops-bootstrap",
		SecurityArtifacts: []securityArtifactConfig{
			{Kind: "sbom", Filename: "aether-ops-bootstrap.spdx.json", Source: "sbom-bootstrap.spdx.json"},
			{Kind: "grype", Filename: "aether-ops-bootstrap.grype.json", Source: "grype-bootstrap.json"},
			{Kind: "vex", Filename: "openvex.json", Source: "openvex.json"},
		},
	}

	ra, pa, err := materializeArtifact(
		srcDir, out, "/aether-ops-bootstrap", "bootstrap", art,
		false /*external*/, nil /*kinds*/, false, /*keepArtifacts*/
	)
	if err != nil {
		t.Fatalf("materializeArtifact: %v", err)
	}

	if got, want := len(ra.SecurityArtifacts), 3; got != want {
		t.Fatalf("rendered security entries = %d, want %d", got, want)
	}
	if got, want := len(pa.SecurityArtifacts), 3; got != want {
		t.Fatalf("public security entries = %d, want %d", got, want)
	}

	stage := filepath.Join(out, "bootstrap", "2026.05.11.1")
	expect := map[string]string{
		"aether-ops-bootstrap.spdx.json":  sbom,
		"aether-ops-bootstrap.grype.json": grype,
		"openvex.json":                    vex,
	}
	for name, body := range expect {
		got, err := os.ReadFile(filepath.Join(stage, name))
		if err != nil {
			t.Errorf("missing staged file %s: %v", name, err)
			continue
		}
		if string(got) != body {
			t.Errorf("file %s body mismatch", name)
		}
		hashRaw := sha256.Sum256([]byte(body))
		wantHash := hex.EncodeToString(hashRaw[:])
		sidecar, err := os.ReadFile(filepath.Join(stage, name+".sha256"))
		if err != nil {
			t.Errorf("missing sidecar %s.sha256: %v", name, err)
			continue
		}
		if !strings.HasPrefix(string(sidecar), wantHash) {
			t.Errorf("sidecar for %s = %q, want prefix %q", name, sidecar, wantHash)
		}
	}

	// Spot-check one rendered entry to confirm URL composition and
	// label fallback (we left Label empty, so the default map kicks
	// in).
	var sbomEntry renderedSecurityArtifact
	for _, e := range ra.SecurityArtifacts {
		if e.Kind == "sbom" {
			sbomEntry = e
			break
		}
	}
	if sbomEntry.Label != "SBOM (SPDX-JSON)" {
		t.Errorf("default SBOM label = %q, want %q", sbomEntry.Label, "SBOM (SPDX-JSON)")
	}
	if sbomEntry.URL != "/aether-ops-bootstrap/bootstrap/2026.05.11.1/aether-ops-bootstrap.spdx.json" {
		t.Errorf("SBOM URL = %q", sbomEntry.URL)
	}
	if sbomEntry.SHA256URL != sbomEntry.URL+".sha256" {
		t.Errorf("SHA256URL not adjacent to URL: %q vs %q", sbomEntry.SHA256URL, sbomEntry.URL)
	}
}

// TestMaterializeSecurityArtifacts_KeepExisting verifies the
// republish path: --keep-existing-artifacts reads each security
// sidecar's SHA from disk and does NOT touch the source file. The
// canonical use case is re-rendering HTML against an already-published
// artifact tree without re-uploading multi-GB bundles or recomputing
// hashes.
func TestMaterializeSecurityArtifacts_KeepExisting(t *testing.T) {
	out := t.TempDir()
	const (
		path  = "2026.05.11.1"
		fname = "aether-ops-bootstrap.spdx.json"
		hash  = "feedfacecafebeef0000000000000000000000000000000000000000beefcafe"
	)
	stage := filepath.Join(out, "bootstrap", path)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, fname), []byte("preexisting sbom body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, fname+".sha256"), []byte(hash+"  "+fname+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	art := artifactConfig{
		Version:  path,
		Path:     path,
		Filename: "aether-ops-bootstrap",
		Source:   "/nonexistent",
		SecurityArtifacts: []securityArtifactConfig{
			{Kind: "sbom", Filename: fname, Source: "/nonexistent-too"},
		},
	}

	rendered, public, err := materializeSecurityArtifacts(
		"" /*metadataDir*/, out, "/aether-ops-bootstrap", "bootstrap", art,
		false /*external*/, true, /*keepArtifacts*/
	)
	if err != nil {
		t.Fatalf("materializeSecurityArtifacts: %v", err)
	}
	if len(rendered) != 1 || rendered[0].SHA256 != hash {
		t.Errorf("rendered = %+v, want one entry with hash %s", rendered, hash)
	}
	if len(public) != 1 || public[0].SHA256 != hash {
		t.Errorf("public = %+v, want one entry with hash %s", public, hash)
	}
}

// TestValidateArtifact_SecurityArtifactsExternalRequiresSHA covers the
// schema contract: external (already-published, no source: pointer)
// security entries must declare sha256 in YAML, since the renderer
// can't recompute it from a missing source.
func TestValidateArtifact_SecurityArtifactsExternalRequiresSHA(t *testing.T) {
	art := artifactConfig{
		Version:  "1",
		Path:     "1",
		Filename: "bin",
		SHA256:   "primarysha",
		SecurityArtifacts: []securityArtifactConfig{
			{Kind: "sbom", Filename: "bin.spdx.json"},
		},
	}
	err := validateArtifact("bootstrap", art, true /*external*/)
	if err == nil || !strings.Contains(err.Error(), "sha256 is required for external releases") {
		t.Fatalf("expected external+missing-sha rejection, got %v", err)
	}
}

// TestValidateArtifact_SecurityArtifactsCollidesWithPrimary blocks the
// foot-gun where someone names a sidecar identically to the primary
// artifact and silently overwrites it during staging.
func TestValidateArtifact_SecurityArtifactsCollidesWithPrimary(t *testing.T) {
	art := artifactConfig{
		Version:  "1",
		Path:     "1",
		Filename: "bin",
		Source:   "src",
		SecurityArtifacts: []securityArtifactConfig{
			{Kind: "sbom", Filename: "bin", Source: "src-sbom"},
		},
	}
	err := validateArtifact("bootstrap", art, false /*external*/)
	if err == nil || !strings.Contains(err.Error(), "collides with the primary artifact") {
		t.Fatalf("expected collision rejection, got %v", err)
	}
}
