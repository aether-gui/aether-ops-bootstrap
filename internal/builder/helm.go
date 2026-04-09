package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// FetchHelm downloads the Helm binary from GitHub releases, verifies
// its checksum, and stages it for inclusion in the bundle.
func FetchHelm(ctx context.Context, dl *Downloader, spec *bundle.HelmSpec, stageDir string) (*bundle.HelmEntry, error) {
	helmDir := filepath.Join(stageDir, "helm")
	if err := os.MkdirAll(helmDir, 0755); err != nil {
		return nil, fmt.Errorf("creating helm staging dir: %w", err)
	}

	version := spec.Version
	archiveName := fmt.Sprintf("helm-%s-linux-amd64.tar.gz", version)
	baseURL := "https://get.helm.sh"

	// Download checksum. Helm's get.helm.sh publishes a bare SHA256 hash
	// (no filename) at {archive}.sha256.
	checksumURL := fmt.Sprintf("%s/%s.sha256", baseURL, archiveName)
	checksumPath := filepath.Join(helmDir, "checksum")
	if _, err := dl.Download(ctx, checksumURL, checksumPath); err != nil {
		return nil, fmt.Errorf("downloading helm checksum: %w", err)
	}
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		return nil, err
	}
	expectedHash := strings.TrimSpace(string(checksumData))
	os.Remove(checksumPath)

	// Download archive.
	archiveURL := fmt.Sprintf("%s/%s", baseURL, archiveName)
	archivePath := filepath.Join(helmDir, archiveName)
	if _, err := dl.Download(ctx, archiveURL, archivePath); err != nil {
		return nil, fmt.Errorf("downloading helm archive: %w", err)
	}

	// Verify checksum.
	if err := VerifyArtifact(archivePath, expectedHash); err != nil {
		return nil, err
	}
	log.Printf("verified %s", archiveName)

	// Extract helm binary. The archive contains linux-amd64/helm.
	binaryPath := filepath.Join(helmDir, "helm")
	if err := extractFileFromTarGz(archivePath, "helm", binaryPath); err != nil {
		return nil, fmt.Errorf("extracting helm binary: %w", err)
	}
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return nil, err
	}
	os.Remove(archivePath)

	// Build manifest entry.
	info, err := os.Stat(binaryPath)
	if err != nil {
		return nil, err
	}
	var hash string
	if err := computeFileSHA256(binaryPath, &hash); err != nil {
		return nil, err
	}

	return &bundle.HelmEntry{
		Version: version,
		Files: []bundle.BundleFile{
			{
				Path:   "helm/helm",
				SHA256: hash,
				Size:   info.Size(),
			},
		},
	}, nil
}
