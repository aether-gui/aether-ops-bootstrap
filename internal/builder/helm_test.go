package builder

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

func TestFetchHelm(t *testing.T) {
	helmBinary := []byte("fake helm binary content")
	archiveBytes := createTarGzBytes(t, map[string]string{
		"linux-amd64/helm":    string(helmBinary),
		"linux-amd64/LICENSE": "license text",
		"linux-amd64/README":  "readme text",
	})

	archiveHash := sha256hex(archiveBytes)
	checksumContent := fmt.Sprintf("%s  helm-v3.17.3-linux-amd64.tar.gz\n", archiveHash)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/helm/helm/releases/download/v3.17.3/helm-v3.17.3-linux-amd64.tar.gz.sha256sum":
			fmt.Fprint(w, checksumContent)
		case "/helm/helm/releases/download/v3.17.3/helm-v3.17.3-linux-amd64.tar.gz":
			_, _ = w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Test the individual pieces since FetchHelm hardcodes github.com URLs.
	dl := &Downloader{Client: srv.Client()}
	dir := t.TempDir()

	// Download and parse checksum.
	checksumPath := filepath.Join(dir, "checksum")
	if _, err := dl.Download(context.Background(), srv.URL+"/helm/helm/releases/download/v3.17.3/helm-v3.17.3-linux-amd64.tar.gz.sha256sum", checksumPath); err != nil {
		t.Fatalf("downloading checksum: %v", err)
	}
	checksums, err := ParseChecksumFile(checksumPath)
	if err != nil {
		t.Fatalf("parsing checksum: %v", err)
	}
	if checksums["helm-v3.17.3-linux-amd64.tar.gz"] != archiveHash {
		t.Errorf("checksum mismatch")
	}

	// Download archive and extract.
	archivePath := filepath.Join(dir, "helm.tar.gz")
	if _, err := dl.Download(context.Background(), srv.URL+"/helm/helm/releases/download/v3.17.3/helm-v3.17.3-linux-amd64.tar.gz", archivePath); err != nil {
		t.Fatalf("downloading archive: %v", err)
	}
	if err := VerifyArtifact(archivePath, archiveHash); err != nil {
		t.Fatalf("verify: %v", err)
	}

	binaryPath := filepath.Join(dir, "helm")
	if err := extractFileFromTarGz(archivePath, "helm", binaryPath); err != nil {
		t.Fatalf("extract: %v", err)
	}

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(helmBinary) {
		t.Error("binary content mismatch")
	}
}

func TestFetchHelmSpec(t *testing.T) {
	spec := &bundle.HelmSpec{Version: "v3.17.3"}
	if spec.Version != "v3.17.3" {
		t.Errorf("Version = %q", spec.Version)
	}
}
