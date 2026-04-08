package builder

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/deb"
)

func TestParsePackagesGz(t *testing.T) {
	// Create a gzipped Packages file.
	content := `Package: git
Version: 1:2.43.0-1ubuntu7
Architecture: amd64
Depends: libc6 (>= 2.38)
Filename: pool/main/g/git/git_2.43.0-1ubuntu7_amd64.deb
Size: 3673594
SHA256: abc123
Priority: optional

Package: libc6
Version: 2.39-0ubuntu8
Architecture: amd64
Filename: pool/main/g/glibc/libc6_2.39-0ubuntu8_amd64.deb
Size: 3200000
SHA256: def456
Priority: required
Essential: yes

`
	dir := t.TempDir()
	gzPath := filepath.Join(dir, "Packages.gz")
	writeGzFile(t, gzPath, content)

	pkgs, err := parsePackagesGz(gzPath)
	if err != nil {
		t.Fatalf("parsePackagesGz: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("len(pkgs) = %d, want 2", len(pkgs))
	}
	if pkgs[0].Name != "git" {
		t.Errorf("pkgs[0].Name = %q", pkgs[0].Name)
	}
	if pkgs[1].Essential {
		// libc6 is Essential in this test data.
		if pkgs[1].Name != "libc6" {
			t.Errorf("pkgs[1].Name = %q", pkgs[1].Name)
		}
	}
}

func TestFetchDebsIntegration(t *testing.T) {
	// Create fake .deb content.
	gitDeb := []byte("fake git deb content")
	gitHash := sha256Hex(gitDeb)

	// Create a Packages index with git depending on nothing (simplified).
	packagesContent := fmt.Sprintf(`Package: git
Version: 1:2.43.0-1ubuntu7
Architecture: amd64
Filename: pool/main/g/git/git_2.43.0-1ubuntu7_amd64.deb
Size: %d
SHA256: %s
Priority: optional

`, len(gitDeb), gitHash)

	packagesGz := gzipBytes(t, packagesContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dists/noble/main/binary-amd64/Packages.gz":
			_, _ = w.Write(packagesGz)
		case "/dists/noble/main/binary-all/Packages.gz",
			"/dists/noble/universe/binary-amd64/Packages.gz",
			"/dists/noble/universe/binary-all/Packages.gz":
			// Empty but valid gzipped Packages.
			_, _ = w.Write(gzipBytes(t, ""))
		case "/pool/main/g/git/git_2.43.0-1ubuntu7_amd64.deb":
			_, _ = w.Write(gitDeb)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Override the archive base URL for testing.
	origBase := ubuntuArchiveBase
	// We can't easily override the const, so test parsePackagesGz and
	// the resolution separately. For a full integration test, we'd need
	// to make the base URL configurable. For now, test the pieces.

	// Test: parse the served Packages.gz.
	dir := t.TempDir()
	gzPath := filepath.Join(dir, "Packages.gz")
	dl := &Downloader{Client: srv.Client()}

	if _, err := dl.Download(context.Background(), srv.URL+"/dists/noble/main/binary-amd64/Packages.gz", gzPath); err != nil {
		t.Fatalf("downloading Packages.gz: %v", err)
	}

	pkgs, err := parsePackagesGz(gzPath)
	if err != nil {
		t.Fatalf("parsePackagesGz: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("len(pkgs) = %d, want 1", len(pkgs))
	}

	// Test: resolve with index.
	idx := deb.NewIndex(pkgs)
	resolved, err := deb.Resolve([]string{"git"}, idx, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Name != "git" {
		t.Fatalf("resolved = %v, want [git]", resolved)
	}

	// Test: download and verify the .deb.
	debPath := filepath.Join(dir, "git.deb")
	if _, err := dl.Download(context.Background(), srv.URL+"/"+resolved[0].Filename, debPath); err != nil {
		t.Fatalf("downloading deb: %v", err)
	}
	if err := VerifyArtifact(debPath, gitHash); err != nil {
		t.Fatalf("verify: %v", err)
	}

	_ = origBase // referenced to avoid unused warning
}

func writeGzFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func gzipBytes(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
