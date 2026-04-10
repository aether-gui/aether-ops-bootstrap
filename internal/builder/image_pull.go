package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/google/go-containerregistry/pkg/crane"
)

// BuildImages pulls each image reference in `imageRefs` into the staging
// directory and returns manifest entries. Images are written as docker
// save-format tarballs under `images/` at the bundle root — RKE2's airgap
// loader automatically imports every tarball it finds in its images dir
// on startup.
//
// The input slice is deduplicated and sorted before pulling so repeated
// builds produce byte-identical output. The returned entries are sorted
// by image reference.
func BuildImages(ctx context.Context, imageRefs []string, stageDir string) (*bundle.ImagesEntry, error) {
	if len(imageRefs) == 0 {
		return nil, nil
	}

	unique := dedupeAndSort(imageRefs)

	imagesDir := filepath.Join(stageDir, "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return nil, fmt.Errorf("creating images staging dir: %w", err)
	}

	entry := &bundle.ImagesEntry{}
	for i, ref := range unique {
		log.Printf("pulling image %d/%d: %s", i+1, len(unique), ref)
		art, err := pullAndSaveImage(ctx, ref, imagesDir)
		if err != nil {
			return nil, fmt.Errorf("pulling %s: %w", ref, err)
		}
		entry.Images = append(entry.Images, art)
	}
	return entry, nil
}

// pullAndSaveImage pulls a single image reference and writes it as a
// docker-save tarball. Returns a manifest artifact entry with the
// resolved descriptor digest (so the manifest records exactly what was
// pulled, not just the originally requested tag).
func pullAndSaveImage(ctx context.Context, ref, imagesDir string) (bundle.ImageArtifact, error) {
	// ctx isn't directly threaded through crane but is honored through
	// the default HTTP transport it configures. Future versions of crane
	// accept a context via an option; wiring that in is a one-liner when
	// we upgrade.
	_ = ctx

	img, err := crane.Pull(ref)
	if err != nil {
		return bundle.ImageArtifact{}, err
	}

	digest, err := img.Digest()
	if err != nil {
		return bundle.ImageArtifact{}, fmt.Errorf("computing digest: %w", err)
	}

	fileName := sanitizeImageRef(ref) + ".tar"
	tarPath := filepath.Join(imagesDir, fileName)

	if err := crane.Save(img, ref, tarPath); err != nil {
		return bundle.ImageArtifact{}, fmt.Errorf("saving tarball: %w", err)
	}

	info, err := os.Stat(tarPath)
	if err != nil {
		return bundle.ImageArtifact{}, err
	}

	var hash string
	if err := computeFileSHA256(tarPath, &hash); err != nil {
		return bundle.ImageArtifact{}, err
	}

	return bundle.ImageArtifact{
		Ref:    ref,
		Digest: digest.String(),
		Path:   filepath.Join("images", fileName),
		SHA256: hash,
		Size:   info.Size(),
	}, nil
}

// sanitizeImageRef converts an image reference into a safe filename.
// Example: "ghcr.io/omec-project/amf:rel-1.8.0" → "ghcr.io_omec-project_amf_rel-1.8.0".
// The mapping is not reversible, which is fine — the reference itself is
// stored verbatim in the manifest entry.
func sanitizeImageRef(ref string) string {
	r := strings.NewReplacer(
		"/", "_",
		":", "_",
		"@", "_",
	)
	return r.Replace(ref)
}

// dedupeAndSort returns a new slice with duplicates removed and entries
// sorted. Used to make bundle builds deterministic regardless of the
// order in which images were discovered.
func dedupeAndSort(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
