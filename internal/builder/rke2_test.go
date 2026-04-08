package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

func TestResolveRKE2ArtifactsAllInOne(t *testing.T) {
	spec := &bundle.RKE2Spec{
		Version:   "v1.33.1+rke2r1",
		Variants:  []string{"canal"},
		ImageMode: bundle.ImageModeAllInOne,
		Source:    bundle.DefaultRKE2Source,
	}

	artifacts := ResolveRKE2Artifacts(spec, []string{"amd64"})

	if len(artifacts) != 3 {
		t.Fatalf("len(artifacts) = %d, want 3", len(artifacts))
	}

	// Binary
	if artifacts[0].Type != "binary" {
		t.Errorf("artifacts[0].Type = %q, want %q", artifacts[0].Type, "binary")
	}
	wantURL := "https://github.com/rancher/rke2/releases/download/v1.33.1%2Brke2r1/rke2.linux-amd64.tar.gz"
	if artifacts[0].URL != wantURL {
		t.Errorf("binary URL = %q, want %q", artifacts[0].URL, wantURL)
	}

	// Images (all-in-one)
	if artifacts[1].Type != "images" {
		t.Errorf("artifacts[1].Type = %q, want %q", artifacts[1].Type, "images")
	}
	if artifacts[1].Filename != "rke2-images.linux-amd64.tar.zst" {
		t.Errorf("images filename = %q", artifacts[1].Filename)
	}

	// Checksum
	if artifacts[2].Type != "checksum" {
		t.Errorf("artifacts[2].Type = %q, want %q", artifacts[2].Type, "checksum")
	}
}

func TestResolveRKE2ArtifactsCoreVariant(t *testing.T) {
	spec := &bundle.RKE2Spec{
		Version:   "v1.33.1+rke2r1",
		Variants:  []string{"canal"},
		ImageMode: bundle.ImageModeCoreVariant,
		Source:    bundle.DefaultRKE2Source,
	}

	artifacts := ResolveRKE2Artifacts(spec, []string{"amd64"})

	// binary + core images + canal images + checksum = 4
	if len(artifacts) != 4 {
		t.Fatalf("len(artifacts) = %d, want 4", len(artifacts))
	}

	if artifacts[1].Filename != "rke2-images-core.linux-amd64.tar.zst" {
		t.Errorf("core images filename = %q", artifacts[1].Filename)
	}
	if artifacts[2].Filename != "rke2-images-canal.linux-amd64.tar.zst" {
		t.Errorf("canal images filename = %q", artifacts[2].Filename)
	}
}

func TestResolveRKE2ArtifactsCustomSource(t *testing.T) {
	spec := &bundle.RKE2Spec{
		Version:   "v1.33.1+rke2r1",
		Variants:  []string{"canal"},
		ImageMode: bundle.ImageModeAllInOne,
		Source:    "https://mirror.internal/rke2",
	}

	artifacts := ResolveRKE2Artifacts(spec, []string{"amd64"})

	for _, a := range artifacts {
		if a.URL[:30] != "https://mirror.internal/rke2/v" {
			t.Errorf("URL should use custom source: %s", a.URL)
		}
	}
}

func TestParseChecksumFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sha256sum-amd64.txt")

	content := `abc123def456  rke2.linux-amd64.tar.gz
789abc012345  rke2-images.linux-amd64.tar.zst
fedcba987654  sha256sum-amd64.txt
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	checksums, err := ParseChecksumFile(path)
	if err != nil {
		t.Fatalf("ParseChecksumFile: %v", err)
	}

	if len(checksums) != 3 {
		t.Fatalf("len(checksums) = %d, want 3", len(checksums))
	}
	if checksums["rke2.linux-amd64.tar.gz"] != "abc123def456" {
		t.Errorf("binary hash = %q", checksums["rke2.linux-amd64.tar.gz"])
	}
}

func TestParseChecksumFileMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")

	content := `abc123  file1.tar.gz

# comment line
short

def456  file2.tar.gz
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	checksums, err := ParseChecksumFile(path)
	if err != nil {
		t.Fatalf("ParseChecksumFile: %v", err)
	}

	if len(checksums) != 2 {
		t.Fatalf("len(checksums) = %d, want 2", len(checksums))
	}
}

func TestVerifyArtifact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	data := []byte("test content for hashing")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	h := sha256.Sum256(data)
	correctHash := hex.EncodeToString(h[:])

	if err := VerifyArtifact(path, correctHash); err != nil {
		t.Fatalf("VerifyArtifact with correct hash: %v", err)
	}

	if err := VerifyArtifact(path, "badhash"); err == nil {
		t.Fatal("VerifyArtifact should fail with wrong hash")
	}
}

func TestFetchAndVerifyRKE2(t *testing.T) {
	// Create fake artifact content.
	binaryContent := []byte("fake rke2 binary tarball")
	imagesContent := []byte("fake rke2 images tarball")

	binaryHash := sha256sum(binaryContent)
	imagesHash := sha256sum(imagesContent)

	checksumContent := fmt.Sprintf("%s  rke2.linux-amd64.tar.gz\n%s  rke2-images.linux-amd64.tar.zst\n",
		binaryHash, imagesHash)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use RawPath because the version contains %2B which the
		// HTTP server decodes to + in r.URL.Path.
		rawPath := r.URL.RawPath
		if rawPath == "" {
			rawPath = r.URL.Path
		}
		switch {
		case rawPath == "/v1.33.1%2Brke2r1/rke2.linux-amd64.tar.gz":
			_, _ = w.Write(binaryContent)
		case rawPath == "/v1.33.1%2Brke2r1/rke2-images.linux-amd64.tar.zst":
			_, _ = w.Write(imagesContent)
		case rawPath == "/v1.33.1%2Brke2r1/sha256sum-amd64.txt":
			fmt.Fprint(w, checksumContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	spec := &bundle.RKE2Spec{
		Version:   "v1.33.1+rke2r1",
		Variants:  []string{"canal"},
		ImageMode: bundle.ImageModeAllInOne,
		Source:    srv.URL,
	}

	dl := &Downloader{Client: srv.Client()}
	stageDir := t.TempDir()

	entry, err := FetchAndVerifyRKE2(context.Background(), dl, spec, []string{"amd64"}, stageDir)
	if err != nil {
		t.Fatalf("FetchAndVerifyRKE2: %v", err)
	}

	if entry.Version != "v1.33.1+rke2r1" {
		t.Errorf("Version = %q", entry.Version)
	}
	if entry.ImageMode != bundle.ImageModeAllInOne {
		t.Errorf("ImageMode = %q", entry.ImageMode)
	}
	if len(entry.Artifacts) != 3 {
		t.Fatalf("len(Artifacts) = %d, want 3", len(entry.Artifacts))
	}

	// Verify artifacts have correct types and non-empty hashes.
	types := map[string]bool{}
	for _, a := range entry.Artifacts {
		types[a.Type] = true
		if a.SHA256 == "" {
			t.Errorf("artifact %s has empty SHA256", a.Path)
		}
		if a.Size == 0 {
			t.Errorf("artifact %s has zero size", a.Path)
		}
	}
	for _, want := range []string{"binary", "images", "checksum"} {
		if !types[want] {
			t.Errorf("missing artifact type %q", want)
		}
	}

	// Verify files exist on disk.
	for _, name := range []string{"rke2.linux-amd64.tar.gz", "rke2-images.linux-amd64.tar.zst", "sha256sum-amd64.txt"} {
		path := filepath.Join(stageDir, "rke2", name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s: %v", name, err)
		}
	}
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
