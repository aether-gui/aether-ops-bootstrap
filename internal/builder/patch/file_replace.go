package patch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ReplaceFile overwrites the regular file at RelPath with Body.
//
// The existing file's mode is preserved unless Mode is non-nil. The
// target must already exist; ReplaceFile does not implicitly create
// missing files (matching the rest of this package's "no silent
// drift" doctrine — a stale RelPath should fail loudly so upstream
// renames don't ship a no-op patch).
//
// Symlinks are rejected: writing through a symlink risks escaping
// rootDir, and rewriting a symlink target is rarely what callers
// want from a "replace this file" action.
type ReplaceFile struct {
	RelPath string
	Body    []byte
	Mode    *fs.FileMode
}

func (r ReplaceFile) Name() string {
	return fmt.Sprintf("replace %s", r.RelPath)
}

func (r ReplaceFile) Apply(rootDir string) error {
	if r.RelPath == "" {
		return errors.New("RelPath is empty")
	}
	full := filepath.Join(rootDir, r.RelPath)

	info, err := os.Lstat(full)
	if err != nil {
		return fmt.Errorf("stat %s: %w", full, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to rewrite", full)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", full)
	}

	mode := info.Mode().Perm()
	if r.Mode != nil {
		mode = (*r.Mode).Perm()
	}

	if err := os.WriteFile(full, r.Body, mode); err != nil {
		return err
	}
	// os.WriteFile only applies the mode to newly-created files; an
	// existing file keeps its previous bits. Chmod explicitly so the
	// caller's intent (or the preserved original mode) is honored.
	return os.Chmod(full, mode)
}
