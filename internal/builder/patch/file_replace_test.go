package patch

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceFileRewritesContent(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "templates/x.conf", "old\n")

	action := ReplaceFile{
		RelPath: "templates/x.conf",
		Body:    []byte("new\n"),
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := readFile(t, rootDir, "templates/x.conf"); got != "new\n" {
		t.Errorf("content = %q, want %q", got, "new\n")
	}
}

func TestReplaceFilePreservesMode(t *testing.T) {
	rootDir := t.TempDir()
	full := filepath.Join(rootDir, "scripts/run")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("#!/bin/sh\n"), 0o750); err != nil {
		t.Fatal(err)
	}

	action := ReplaceFile{
		RelPath: "scripts/run",
		Body:    []byte("#!/bin/sh\necho hi\n"),
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	info, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Errorf("mode = %o, want 0750", got)
	}
}

func TestReplaceFileExplicitModeOverrides(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "etc/x.conf", "old\n")

	mode := fs.FileMode(0o600)
	action := ReplaceFile{
		RelPath: "etc/x.conf",
		Body:    []byte("new\n"),
		Mode:    &mode,
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	info, err := os.Stat(filepath.Join(rootDir, "etc/x.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 0600", got)
	}
}

func TestReplaceFileIdempotent(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "x.conf", "old\n")

	action := ReplaceFile{RelPath: "x.conf", Body: []byte("v2\n")}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := action.Apply(rootDir); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if got := readFile(t, rootDir, "x.conf"); got != "v2\n" {
		t.Errorf("content = %q after idempotent re-apply", got)
	}
}

func TestReplaceFileErrorsOnMissingTarget(t *testing.T) {
	rootDir := t.TempDir()
	action := ReplaceFile{RelPath: "missing.conf", Body: []byte("x")}
	err := action.Apply(rootDir)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !strings.Contains(err.Error(), "missing.conf") {
		t.Errorf("error should mention the missing path; got %v", err)
	}
}

func TestReplaceFileRejectsSymlink(t *testing.T) {
	rootDir := t.TempDir()
	writeFile(t, rootDir, "real.conf", "real\n")
	if err := os.Symlink(filepath.Join(rootDir, "real.conf"), filepath.Join(rootDir, "link.conf")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	action := ReplaceFile{RelPath: "link.conf", Body: []byte("x\n")}
	err := action.Apply(rootDir)
	if err == nil {
		t.Fatal("expected error rewriting through symlink")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error should mention symlink; got %v", err)
	}
	// Real file behind the symlink must be untouched.
	if got := readFile(t, rootDir, "real.conf"); got != "real\n" {
		t.Errorf("symlink target was rewritten: %q", got)
	}
}

func TestReplaceFileErrorsOnEmptyRelPath(t *testing.T) {
	rootDir := t.TempDir()
	action := ReplaceFile{Body: []byte("x")}
	if err := action.Apply(rootDir); err == nil {
		t.Fatal("expected error for empty RelPath")
	}
}
