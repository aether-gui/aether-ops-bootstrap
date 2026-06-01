package archive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveRoundTrip(t *testing.T) {
	srcDir := t.TempDir()

	// Create test files.
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "rke2"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "rke2", "binary.tar.gz"), []byte("binary data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "rke2", "images.tar.zst"), []byte("image data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Archive.
	archivePath := filepath.Join(t.TempDir(), "test.tar.zst")
	if err := Archive(srcDir, archivePath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Verify archive file exists and is non-empty.
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("archive file missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("archive file is empty")
	}

	// Unarchive.
	destDir := t.TempDir()
	if err := Unarchive(archivePath, destDir); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}

	// Verify contents.
	got, err := os.ReadFile(filepath.Join(destDir, "manifest.json"))
	if err != nil {
		t.Fatalf("reading manifest.json: %v", err)
	}
	if string(got) != `{"version":1}` {
		t.Errorf("manifest.json content = %q", got)
	}

	got, err = os.ReadFile(filepath.Join(destDir, "rke2", "binary.tar.gz"))
	if err != nil {
		t.Fatalf("reading rke2/binary.tar.gz: %v", err)
	}
	if string(got) != "binary data" {
		t.Errorf("binary.tar.gz content = %q", got)
	}

	got, err = os.ReadFile(filepath.Join(destDir, "rke2", "images.tar.zst"))
	if err != nil {
		t.Fatalf("reading rke2/images.tar.zst: %v", err)
	}
	if string(got) != "image data" {
		t.Errorf("images.tar.zst content = %q", got)
	}
}

func TestReadFileFromArchive(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "rke2"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "rke2", "binary.tar.gz"), []byte("binary data"), 0644); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(t.TempDir(), "test.tar.zst")
	if err := Archive(srcDir, archivePath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	got, err := ReadFileFromArchive(archivePath, "manifest.json")
	if err != nil {
		t.Fatalf("ReadFileFromArchive: %v", err)
	}
	if string(got) != `{"version":1}` {
		t.Errorf("manifest content = %q", got)
	}

	// Nested entry uses forward slashes regardless of host OS.
	got, err = ReadFileFromArchive(archivePath, "rke2/binary.tar.gz")
	if err != nil {
		t.Fatalf("ReadFileFromArchive nested: %v", err)
	}
	if string(got) != "binary data" {
		t.Errorf("binary content = %q", got)
	}

	_, err = ReadFileFromArchive(archivePath, "does-not-exist.json")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist for missing entry, got %v", err)
	}
}
