package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteBundleChecksum computes the SHA256 of the archive at archivePath
// and writes it to a sidecar file at archivePath + ".sha256" in GNU
// coreutils format. Returns the hex-encoded hash.
func WriteBundleChecksum(archivePath string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("opening archive for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("computing archive checksum: %w", err)
	}

	hash := hex.EncodeToString(h.Sum(nil))
	basename := filepath.Base(archivePath)
	checksumContent := fmt.Sprintf("%s  %s\n", hash, basename)

	checksumPath := archivePath + ".sha256"
	if err := os.WriteFile(checksumPath, []byte(checksumContent), 0644); err != nil {
		return "", fmt.Errorf("writing checksum file: %w", err)
	}

	return hash, nil
}
