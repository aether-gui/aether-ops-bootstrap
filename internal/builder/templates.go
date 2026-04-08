package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// StageTemplates copies files from templatesDir into the staging area
// under templates/, computing SHA256 and size for each. Returns nil if
// no template files are found (e.g., directory contains only .gitkeep).
func StageTemplates(templatesDir, stageDir string) (*bundle.TemplatesEntry, error) {
	destDir := filepath.Join(stageDir, "templates")
	var files []bundle.BundleFile

	err := filepath.WalkDir(templatesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == ".gitkeep" {
			return nil
		}

		rel, err := filepath.Rel(templatesDir, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		// Copy file.
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.Create(destPath)
		if err != nil {
			return err
		}

		h := sha256.New()
		w := io.MultiWriter(out, h)
		n, copyErr := io.Copy(w, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}

		files = append(files, bundle.BundleFile{
			Path:   filepath.ToSlash(filepath.Join("templates", rel)),
			SHA256: hex.EncodeToString(h.Sum(nil)),
			Size:   n,
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("staging templates from %s: %w", templatesDir, err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	return &bundle.TemplatesEntry{Files: files}, nil
}
