package builder

import (
	"archive/tar"
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

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

func TestExtractFileFromTarGz(t *testing.T) {
	// Create a tar.gz with a known file.
	archivePath := filepath.Join(t.TempDir(), "test.tar.gz")
	createTarGz(t, archivePath, map[string]string{
		"aether-ops": "binary content",
		"README.md":  "readme content",
		"LICENSE":    "license content",
	})

	destPath := filepath.Join(t.TempDir(), "extracted", "aether-ops")
	if err := extractFileFromTarGz(archivePath, "aether-ops", destPath); err != nil {
		t.Fatalf("extractFileFromTarGz: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary content" {
		t.Errorf("content = %q, want %q", got, "binary content")
	}
}

func TestExtractFileFromTarGzMissing(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "test.tar.gz")
	createTarGz(t, archivePath, map[string]string{
		"README.md": "readme",
	})

	destPath := filepath.Join(t.TempDir(), "missing")
	if err := extractFileFromTarGz(archivePath, "aether-ops", destPath); err == nil {
		t.Fatal("should fail when file not in archive")
	}
}

func TestExecCmdSuccess(t *testing.T) {
	// Use "go version" which is always available during go test.
	if err := execCmd(context.Background(), "", "go", "version"); err != nil {
		t.Fatalf("execCmd: %v", err)
	}
}

func TestExecCmdFailure(t *testing.T) {
	// Use "go" with an invalid subcommand to get a non-zero exit.
	if err := execCmd(context.Background(), "", "go", "nosuchcommand"); err == nil {
		t.Fatal("should fail on non-zero exit")
	}
}

func TestCheckSourceBuildTools(t *testing.T) {
	// This test depends on the host having git and go.
	// It's a best-effort check — skip if running in a minimal container.
	err := checkSourceBuildTools()
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "sub", "dst.txt")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Errorf("content = %q, want %q", got, "data")
	}
}

func TestBuildAetherOpsDownloadMode(t *testing.T) {
	binaryContent := []byte("fake aether-ops binary")
	serviceContent := []byte("[Unit]\nDescription=aether-ops\n")

	// Create a tar.gz archive like GoReleaser produces.
	archiveBuf := createTarGzBytes(t, map[string]string{
		"aether-ops": string(binaryContent),
		"README.md":  "readme",
	})

	archiveHash := sha256hex(archiveBuf)
	checksumContent := fmt.Sprintf("%s  aether-ops_0.1.0_linux_amd64.tar.gz\n", archiveHash)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/aether-gui/aether-ops/releases/download/v0.1.0/checksums.txt":
			fmt.Fprint(w, checksumContent)
		case "/aether-gui/aether-ops/releases/download/v0.1.0/aether-ops_0.1.0_linux_amd64.tar.gz":
			_, _ = w.Write(archiveBuf)
		case "/aether-gui/aether-ops/v0.1.0/deploy/systemd/aether-ops.service":
			_, _ = w.Write(serviceContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dl := &Downloader{Client: srv.Client()}
	aopsDir := filepath.Join(t.TempDir(), "aether-ops")
	if err := os.MkdirAll(aopsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Test the release fetcher directly by overriding URLs.
	// Since fetchAetherOpsRelease hardcodes github.com URLs, we test
	// the component parts instead.

	// Test checksum download + parse.
	checksumsPath := filepath.Join(t.TempDir(), "checksums.txt")
	if _, err := dl.Download(context.Background(), srv.URL+"/aether-gui/aether-ops/releases/download/v0.1.0/checksums.txt", checksumsPath); err != nil {
		t.Fatalf("downloading checksums: %v", err)
	}
	checksums, err := ParseChecksumFile(checksumsPath)
	if err != nil {
		t.Fatalf("parsing checksums: %v", err)
	}
	if checksums["aether-ops_0.1.0_linux_amd64.tar.gz"] != archiveHash {
		t.Errorf("checksum mismatch")
	}

	// Test archive download + extract.
	archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	if _, err := dl.Download(context.Background(), srv.URL+"/aether-gui/aether-ops/releases/download/v0.1.0/aether-ops_0.1.0_linux_amd64.tar.gz", archivePath); err != nil {
		t.Fatalf("downloading archive: %v", err)
	}
	if err := VerifyArtifact(archivePath, archiveHash); err != nil {
		t.Fatalf("verify archive: %v", err)
	}

	binaryPath := filepath.Join(aopsDir, "aether-ops")
	if err := extractFileFromTarGz(archivePath, "aether-ops", binaryPath); err != nil {
		t.Fatalf("extract: %v", err)
	}

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binaryContent) {
		t.Errorf("binary content mismatch")
	}
}

func TestBuildAetherOpsLocalBinary(t *testing.T) {
	dir := t.TempDir()

	// Create a fake binary and service file.
	binaryPath := filepath.Join(dir, "aether-ops")
	if err := os.WriteFile(binaryPath, []byte("local binary"), 0755); err != nil {
		t.Fatal(err)
	}
	servicePath := filepath.Join(dir, "aether-ops.service")
	if err := os.WriteFile(servicePath, []byte("[Unit]\nDescription=test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	spec := &bundle.AetherOpsSpec{
		Version: "v1.0.0",
		Source:  binaryPath,
		Repo:    "aether-gui/aether-ops",
	}

	stageDir := t.TempDir()
	entry, err := BuildAetherOps(context.Background(), &Downloader{}, spec, stageDir)
	if err != nil {
		t.Fatalf("BuildAetherOps: %v", err)
	}

	if entry.Version != "v1.0.0" {
		t.Errorf("Version = %q", entry.Version)
	}
	if len(entry.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(entry.Files))
	}

	// Verify files exist in staging.
	for _, name := range []string{"aether-ops", "aether-ops.service"} {
		p := filepath.Join(stageDir, "aether-ops", name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing staged file %s: %v", name, err)
		}
	}
}

// Helper: create a tar.gz file on disk.
func createTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	data := createTarGzBytes(t, files)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// Helper: create a tar.gz in memory.
func createTarGzBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
