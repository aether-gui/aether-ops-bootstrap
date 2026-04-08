package builder

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// BuildAetherOps acquires the aether-ops binary and service file,
// placing them in the staging directory. Dispatches to one of three
// modes based on the spec: local source, source build, or release download.
func BuildAetherOps(ctx context.Context, dl *Downloader, spec *bundle.AetherOpsSpec, stageDir string) (*bundle.AetherOpsEntry, error) {
	aopsDir := filepath.Join(stageDir, "aether-ops")
	if err := os.MkdirAll(aopsDir, 0755); err != nil {
		return nil, fmt.Errorf("creating aether-ops staging dir: %w", err)
	}

	switch {
	case spec.Source != "":
		if err := copyAetherOpsFromLocal(ctx, dl, spec, aopsDir); err != nil {
			return nil, err
		}
	case spec.Ref != "":
		if err := buildAetherOpsFromSource(ctx, spec, aopsDir); err != nil {
			return nil, err
		}
	default:
		if err := fetchAetherOpsRelease(ctx, dl, spec, aopsDir); err != nil {
			return nil, err
		}
	}

	return buildAetherOpsEntry(spec.Version, aopsDir)
}

// buildAetherOpsEntry computes hashes and sizes for the staged files
// and returns a manifest entry.
func buildAetherOpsEntry(version, aopsDir string) (*bundle.AetherOpsEntry, error) {
	var files []bundle.BundleFile
	for _, name := range []string{"aether-ops", "aether-ops.service"} {
		path := filepath.Join(aopsDir, name)
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("expected file %s not found in staging: %w", name, err)
		}

		var hash string
		if err := computeFileSHA256(path, &hash); err != nil {
			return nil, err
		}

		files = append(files, bundle.BundleFile{
			Path:   filepath.Join("aether-ops", name),
			SHA256: hash,
			Size:   info.Size(),
		})
	}

	return &bundle.AetherOpsEntry{
		Version: version,
		Files:   files,
	}, nil
}

// fetchAetherOpsRelease downloads a pre-built release from GitHub.
// GoReleaser archive naming: aether-ops_{versionNoV}_linux_amd64.tar.gz
func fetchAetherOpsRelease(ctx context.Context, dl *Downloader, spec *bundle.AetherOpsSpec, aopsDir string) error {
	versionNoV := strings.TrimPrefix(spec.Version, "v")
	repo := spec.Repo

	archiveName := fmt.Sprintf("aether-ops_%s_linux_amd64.tar.gz", versionNoV)
	baseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s", repo, spec.Version)

	// Download checksums.
	checksumsPath := filepath.Join(aopsDir, "checksums.txt")
	if _, err := dl.Download(ctx, baseURL+"/checksums.txt", checksumsPath); err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	checksums, err := ParseChecksumFile(checksumsPath)
	if err != nil {
		return err
	}
	// Remove checksums file from staging (not part of the bundle).
	os.Remove(checksumsPath)

	// Download and verify archive.
	archivePath := filepath.Join(aopsDir, archiveName)
	if _, err := dl.Download(ctx, baseURL+"/"+archiveName, archivePath); err != nil {
		return fmt.Errorf("downloading release archive: %w", err)
	}

	expectedHash, ok := checksums[archiveName]
	if !ok {
		return fmt.Errorf("no checksum found for %s in checksums.txt", archiveName)
	}
	if err := VerifyArtifact(archivePath, expectedHash); err != nil {
		return err
	}
	log.Printf("verified %s", archiveName)

	// Extract binary from archive.
	if err := extractFileFromTarGz(archivePath, "aether-ops", filepath.Join(aopsDir, "aether-ops")); err != nil {
		return fmt.Errorf("extracting binary from archive: %w", err)
	}
	if err := os.Chmod(filepath.Join(aopsDir, "aether-ops"), 0755); err != nil {
		return err
	}
	os.Remove(archivePath)

	// Download service file from the repo at the tagged version.
	serviceURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/deploy/systemd/aether-ops.service", repo, spec.Version)
	if _, err := dl.Download(ctx, serviceURL, filepath.Join(aopsDir, "aether-ops.service")); err != nil {
		return fmt.Errorf("downloading service file: %w", err)
	}

	return nil
}

// buildAetherOpsFromSource clones the repo and builds from source,
// including the frontend.
func buildAetherOpsFromSource(ctx context.Context, spec *bundle.AetherOpsSpec, aopsDir string) error {
	if err := checkSourceBuildTools(); err != nil {
		return err
	}

	workspace, err := os.MkdirTemp("", "aether-ops-build-*")
	if err != nil {
		return fmt.Errorf("creating build workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	repoURL := fmt.Sprintf("git@github.com:%s.git", spec.Repo)

	// Clone. Try --branch first (works for tags and branches).
	// If it fails (e.g., a commit SHA), clone default and checkout.
	log.Printf("cloning %s at %s", spec.Repo, spec.Ref)
	if err := execCmd(ctx, "", "git", "clone", "--depth", "1", "--branch", spec.Ref, repoURL, workspace); err != nil {
		// Retry without --branch for commit SHAs.
		if err2 := os.RemoveAll(workspace); err2 != nil {
			return fmt.Errorf("cleaning up failed clone: %w", err2)
		}
		if err2 := os.MkdirAll(workspace, 0755); err2 != nil {
			return err2
		}
		if err2 := execCmd(ctx, "", "git", "clone", repoURL, workspace); err2 != nil {
			return fmt.Errorf("cloning %s: %w", spec.Repo, err2)
		}
		if err2 := execCmd(ctx, workspace, "git", "checkout", spec.Ref); err2 != nil {
			return fmt.Errorf("checking out %s: %w", spec.Ref, err2)
		}
	}

	// Initialize submodules.
	log.Printf("initializing submodules")
	if err := execCmd(ctx, workspace, "git", "submodule", "update", "--init", "--recursive"); err != nil {
		return fmt.Errorf("initializing submodules: %w", err)
	}

	// Override frontend ref if specified.
	if spec.FrontendRef != "" {
		frontendDir := filepath.Join(workspace, "web", "frontend")
		log.Printf("checking out frontend at %s", spec.FrontendRef)
		if err := execCmd(ctx, frontendDir, "git", "checkout", spec.FrontendRef); err != nil {
			return fmt.Errorf("checking out frontend %s: %w", spec.FrontendRef, err)
		}
	}

	// Build frontend.
	frontendDir := filepath.Join(workspace, "web", "frontend")
	log.Printf("building frontend (npm install + build)")
	if err := execCmd(ctx, frontendDir, "npm", "install"); err != nil {
		return fmt.Errorf("npm install: %w", err)
	}
	if err := execCmd(ctx, frontendDir, "npm", "run", "build"); err != nil {
		return fmt.Errorf("npm run build: %w", err)
	}

	// Embed frontend.
	embedDir := filepath.Join(workspace, "internal", "frontend", "dist")
	if err := os.RemoveAll(embedDir); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(frontendDir, "dist"), embedDir); err != nil {
		return fmt.Errorf("embedding frontend: %w", err)
	}

	// Resolve ldflags.
	commitHash, _ := execCmdOutput(ctx, workspace, "git", "rev-parse", "--short", "HEAD")
	branch, _ := execCmdOutput(ctx, workspace, "git", "rev-parse", "--abbrev-ref", "HEAD")
	buildDate := time.Now().UTC().Format(time.RFC3339)

	ldflags := fmt.Sprintf("-X 'main.version=%s' -X 'main.commitHash=%s' -X 'main.branch=%s' -X 'main.buildDate=%s'",
		spec.Version, strings.TrimSpace(commitHash), strings.TrimSpace(branch), buildDate)

	// Build binary.
	binaryPath := filepath.Join(aopsDir, "aether-ops")
	log.Printf("building aether-ops binary (CGO_ENABLED=0)")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", ldflags, "-o", binaryPath, "./cmd/aether-ops")
	buildCmd.Dir = workspace
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, output)
	}

	// Copy service file.
	serviceFile := filepath.Join(workspace, "deploy", "systemd", "aether-ops.service")
	if err := copyFile(serviceFile, filepath.Join(aopsDir, "aether-ops.service")); err != nil {
		return fmt.Errorf("copying service file: %w", err)
	}

	return nil
}

// copyAetherOpsFromLocal handles a local pre-built binary or release archive.
func copyAetherOpsFromLocal(ctx context.Context, dl *Downloader, spec *bundle.AetherOpsSpec, aopsDir string) error {
	srcPath := spec.Source

	binaryDest := filepath.Join(aopsDir, "aether-ops")
	if strings.HasSuffix(srcPath, ".tar.gz") {
		// Extract binary from release archive.
		if err := extractFileFromTarGz(srcPath, "aether-ops", binaryDest); err != nil {
			return fmt.Errorf("extracting binary from %s: %w", srcPath, err)
		}
	} else {
		// Copy binary directly.
		if err := copyFile(srcPath, binaryDest); err != nil {
			return fmt.Errorf("copying binary from %s: %w", srcPath, err)
		}
	}
	if err := os.Chmod(binaryDest, 0755); err != nil {
		return err
	}

	// Try to find service file next to the source.
	serviceFile := filepath.Join(filepath.Dir(srcPath), "aether-ops.service")
	serviceDest := filepath.Join(aopsDir, "aether-ops.service")
	if _, err := os.Stat(serviceFile); err == nil {
		return copyFile(serviceFile, serviceDest)
	}

	// Fall back to downloading from GitHub at the specified version.
	serviceURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/deploy/systemd/aether-ops.service", spec.Repo, spec.Version)
	if _, err := dl.Download(ctx, serviceURL, serviceDest); err != nil {
		return fmt.Errorf("downloading service file: %w", err)
	}

	return nil
}

// checkSourceBuildTools verifies that git, go, node, and npm are available.
func checkSourceBuildTools() error {
	required := []string{"git", "go", "node", "npm"}
	var missing []string
	for _, tool := range required {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("source build requires tools not found in PATH: %s", strings.Join(missing, ", "))
	}
	return nil
}

// execCmd runs a command in the given directory and returns an error
// with stderr output on failure.
func execCmd(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, output)
	}
	return nil
}

// execCmdOutput runs a command and returns its stdout.
func execCmdOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.Output()
	return string(output), err
}

// extractFileFromTarGz extracts a single file by name from a tar.gz archive.
func extractFileFromTarGz(archivePath, filename, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("file %q not found in archive %s", filename, filepath.Base(archivePath))
		}
		if err != nil {
			return err
		}

		if filepath.Base(header.Name) == filename && header.Typeflag == tar.TypeReg {
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}
			out, err := os.Create(destPath)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
	}
}

// copyFile copies src to dst, creating parent directories as needed.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// copyDir recursively copies src directory to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

// computeFileSHA256 computes the SHA256 hash of a file.
func computeFileSHA256(path string, out *string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	*out = hex.EncodeToString(h.Sum(nil))
	return nil
}
