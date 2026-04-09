package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteBundleChecksum(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "test-bundle.tar.zst")
	content := []byte("fake archive content for checksum test")
	if err := os.WriteFile(archivePath, content, 0644); err != nil {
		t.Fatal(err)
	}

	hash, err := WriteBundleChecksum(archivePath)
	if err != nil {
		t.Fatalf("WriteBundleChecksum: %v", err)
	}

	// Verify hash matches independently computed value.
	h := sha256.Sum256(content)
	wantHash := hex.EncodeToString(h[:])
	if hash != wantHash {
		t.Errorf("hash = %q, want %q", hash, wantHash)
	}

	// Verify sidecar file content.
	checksumPath := archivePath + ".sha256"
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("reading checksum file: %v", err)
	}
	wantContent := wantHash + "  test-bundle.tar.zst\n"
	if string(data) != wantContent {
		t.Errorf("checksum file content = %q, want %q", data, wantContent)
	}

	// Verify the hash can be parsed back by ParseChecksumFile.
	checksums, err := ParseChecksumFile(checksumPath)
	if err != nil {
		t.Fatalf("ParseChecksumFile: %v", err)
	}
	if checksums["test-bundle.tar.zst"] != wantHash {
		t.Errorf("parsed checksum = %q, want %q", checksums["test-bundle.tar.zst"], wantHash)
	}

}
