package builder

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// ArtifactURL describes a single remote artifact to fetch.
type ArtifactURL struct {
	Type     string // "binary", "images", "checksum"
	Arch     string
	URL      string
	Filename string // basename for staging, e.g. "rke2.linux-amd64.tar.gz"
}

// ResolveRKE2Artifacts computes the download URLs for RKE2 airgap artifacts
// based on the spec and target architectures.
func ResolveRKE2Artifacts(spec *bundle.RKE2Spec, architectures []string) []ArtifactURL {
	encodedVersion := strings.ReplaceAll(spec.Version, "+", "%2B")
	source := spec.Source
	if source == "" {
		source = bundle.DefaultRKE2Source
	}

	var artifacts []ArtifactURL
	for _, arch := range architectures {
		base := fmt.Sprintf("%s/%s", source, encodedVersion)

		// Binary tarball
		binaryFile := fmt.Sprintf("rke2.linux-%s.tar.gz", arch)
		artifacts = append(artifacts, ArtifactURL{
			Type:     "binary",
			Arch:     arch,
			URL:      fmt.Sprintf("%s/%s", base, binaryFile),
			Filename: binaryFile,
		})

		// Image tarballs
		switch spec.ImageMode {
		case bundle.ImageModeCoreVariant:
			coreFile := fmt.Sprintf("rke2-images-core.linux-%s.tar.zst", arch)
			artifacts = append(artifacts, ArtifactURL{
				Type:     "images",
				Arch:     arch,
				URL:      fmt.Sprintf("%s/%s", base, coreFile),
				Filename: coreFile,
			})
			for _, variant := range spec.Variants {
				variantFile := fmt.Sprintf("rke2-images-%s.linux-%s.tar.zst", variant, arch)
				artifacts = append(artifacts, ArtifactURL{
					Type:     "images",
					Arch:     arch,
					URL:      fmt.Sprintf("%s/%s", base, variantFile),
					Filename: variantFile,
				})
			}
		default: // all-in-one
			imagesFile := fmt.Sprintf("rke2-images.linux-%s.tar.zst", arch)
			artifacts = append(artifacts, ArtifactURL{
				Type:     "images",
				Arch:     arch,
				URL:      fmt.Sprintf("%s/%s", base, imagesFile),
				Filename: imagesFile,
			})
		}

		// Checksum file
		checksumFile := fmt.Sprintf("sha256sum-%s.txt", arch)
		artifacts = append(artifacts, ArtifactURL{
			Type:     "checksum",
			Arch:     arch,
			URL:      fmt.Sprintf("%s/%s", base, checksumFile),
			Filename: checksumFile,
		})
	}

	return artifacts
}

// ParseChecksumFile reads a sha256sum file and returns a map of
// basename → hex-encoded SHA256 hash. The file uses GNU coreutils
// format: "<hash>  <filename>" (two spaces).
func ParseChecksumFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening checksum file %s: %w", path, err)
	}
	defer f.Close()

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		name := filepath.Base(fields[1])
		checksums[name] = hash
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading checksum file %s: %w", path, err)
	}

	return checksums, nil
}

// VerifyArtifact checks that the file at path matches the expected SHA256 hash.
func VerifyArtifact(path, expectedSHA256 string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s for verification: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing %s: %w", path, err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expectedSHA256 {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", filepath.Base(path), actual, expectedSHA256)
	}

	return nil
}

// FetchAndVerifyRKE2 resolves, downloads, and verifies RKE2 artifacts,
// returning a manifest entry ready for inclusion in the bundle.
func FetchAndVerifyRKE2(ctx context.Context, dl *Downloader, spec *bundle.RKE2Spec, archs []string, stageDir string) (*bundle.RKE2Entry, error) {
	artifacts := ResolveRKE2Artifacts(spec, archs)

	rke2Dir := filepath.Join(stageDir, "rke2")
	if err := os.MkdirAll(rke2Dir, 0755); err != nil {
		return nil, fmt.Errorf("creating rke2 staging dir: %w", err)
	}

	// Download all artifacts, checksums first so we can verify as we go.
	// Partition into checksum files and data files.
	var checksumArtifacts, dataArtifacts []ArtifactURL
	for _, a := range artifacts {
		if a.Type == "checksum" {
			checksumArtifacts = append(checksumArtifacts, a)
		} else {
			dataArtifacts = append(dataArtifacts, a)
		}
	}

	// Download and parse checksum files.
	allChecksums := make(map[string]string)
	for _, a := range checksumArtifacts {
		dest := filepath.Join(rke2Dir, a.Filename)
		if _, err := dl.Download(ctx, a.URL, dest); err != nil {
			return nil, fmt.Errorf("downloading %s: %w", a.Filename, err)
		}
		parsed, err := ParseChecksumFile(dest)
		if err != nil {
			return nil, err
		}
		for k, v := range parsed {
			allChecksums[k] = v
		}
	}

	// Download and verify data artifacts.
	for _, a := range dataArtifacts {
		dest := filepath.Join(rke2Dir, a.Filename)
		if _, err := dl.Download(ctx, a.URL, dest); err != nil {
			return nil, fmt.Errorf("downloading %s: %w", a.Filename, err)
		}

		expected, ok := allChecksums[a.Filename]
		if !ok {
			return nil, fmt.Errorf("no checksum found for %s in sha256sum file", a.Filename)
		}
		if err := VerifyArtifact(dest, expected); err != nil {
			return nil, err
		}
		log.Printf("verified %s", a.Filename)
	}

	// Build manifest entry from staged files.
	var manifestArtifacts []bundle.RKE2Artifact
	for _, a := range artifacts {
		dest := filepath.Join(rke2Dir, a.Filename)
		info, err := os.Stat(dest)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", a.Filename, err)
		}

		hash := allChecksums[a.Filename]
		if a.Type == "checksum" {
			// Compute hash for the checksum file itself (not in the checksum file).
			if err := computeSHA256(dest, &hash); err != nil {
				return nil, err
			}
		}

		manifestArtifacts = append(manifestArtifacts, bundle.RKE2Artifact{
			Type:   a.Type,
			Arch:   a.Arch,
			Path:   filepath.Join("rke2", a.Filename),
			SHA256: hash,
			Size:   info.Size(),
		})
	}

	return &bundle.RKE2Entry{
		Version:   spec.Version,
		Variants:  spec.Variants,
		ImageMode: spec.ImageMode,
		Artifacts: manifestArtifacts,
	}, nil
}

func computeSHA256(path string, out *string) error {
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
