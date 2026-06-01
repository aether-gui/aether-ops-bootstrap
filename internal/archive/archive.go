package archive

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Archive creates a tar.zst archive at outputPath from the contents of sourceDir.
// File paths in the archive are relative to sourceDir.
func Archive(sourceDir, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating archive %s: %w", outputPath, err)
	}
	defer outFile.Close()

	zw, err := zstd.NewWriter(outFile)
	if err != nil {
		return fmt.Errorf("creating zstd writer: %w", err)
	}

	tw := tar.NewWriter(zw)

	err = filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return fmt.Errorf("walking source directory: %w", err)
	}

	// Close in order: tar → zstd. File closed by defer.
	if err := tw.Close(); err != nil {
		return fmt.Errorf("closing tar writer: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("closing zstd writer: %w", err)
	}

	return nil
}

// ReadFileFromArchive streams a tar.zst archive and returns the raw
// bytes of the first entry whose name matches target. Returns
// os.ErrNotExist when no entry matches, leaving the rest of the
// archive unread on disk.
//
// Used by lightweight readers (e.g. bundle inspection) that need a
// single manifest file without paying the cost of extracting every
// archive member.
func ReadFileFromArchive(archivePath, target string) ([]byte, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("opening archive %s: %w", archivePath, err)
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("creating zstd reader: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s: %w", target, os.ErrNotExist)
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar entry: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.ToSlash(header.Name) != target {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", target, err)
		}
		return data, nil
	}
}

// Unarchive extracts a tar.zst archive to destDir.
// Rejects entries with path traversal (absolute paths or ".." segments).
func Unarchive(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening archive %s: %w", archivePath, err)
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("creating zstd reader: %w", err)
	}
	defer zr.Close()

	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolving dest dir: %w", err)
	}

	tr := tar.NewReader(zr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Reject path traversal.
		clean := filepath.FromSlash(header.Name)
		if filepath.IsAbs(clean) || strings.Contains(clean, "..") {
			return fmt.Errorf("invalid tar entry path: %s", header.Name)
		}
		target := filepath.Join(absDestDir, clean)
		if !strings.HasPrefix(target, absDestDir+string(filepath.Separator)) && target != absDestDir {
			return fmt.Errorf("tar entry escapes destination: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}

	return nil
}
