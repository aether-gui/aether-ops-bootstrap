package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// SchemaVersion is the current manifest schema version.
// The launcher refuses to process manifests with unrecognized schema versions.
const SchemaVersion = 1

// Manifest is the top-level bundle manifest structure.
// It is the contract between the launcher and the bundle: the launcher reads it
// to discover what components are present and where they live in the archive.
type Manifest struct {
	SchemaVersion int           `json:"schema_version"`
	BundleVersion string        `json:"bundle_version"`
	BundleSHA256  string        `json:"bundle_sha256"`
	BuildInfo     BuildInfo     `json:"build_info"`
	Components    ComponentList `json:"components"`
}

// BuildInfo records how and when the bundle was built.
// These fields are informational and excluded from the bundle hash.
type BuildInfo struct {
	GoVersion string `json:"go_version"`
	GitSHA    string `json:"git_sha"`
	Builder   string `json:"builder"`
	Timestamp string `json:"timestamp"`
}

// ComponentList groups all component entries in the manifest.
type ComponentList struct {
	Debs       []DebEntry        `json:"debs,omitempty"`
	RKE2       *RKE2Entry        `json:"rke2,omitempty"`
	Helm       *HelmEntry        `json:"helm,omitempty"`
	Wheelhouse *WheelhouseEntry  `json:"wheelhouse,omitempty"`
	AetherOps  *AetherOpsEntry   `json:"aether_ops,omitempty"`
	Onramp     *OnrampEntry      `json:"onramp,omitempty"`
	HelmCharts []HelmChartsEntry `json:"helm_charts,omitempty"`
	Images     *ImagesEntry      `json:"images,omitempty"`
	Templates  *TemplatesEntry   `json:"templates,omitempty"`
}

// DebEntry describes a vendored .deb package in the bundle.
type DebEntry struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Arch     string `json:"arch"`
	Suite    string `json:"suite"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
}

// RKE2Entry describes the RKE2 artifacts in the bundle.
type RKE2Entry struct {
	Version   string         `json:"version"`
	Variants  []string       `json:"variants"`
	ImageMode string         `json:"image_mode"`
	Artifacts []RKE2Artifact `json:"artifacts"`
}

// RKE2Artifact is a single RKE2 file in the bundle. The Type field tells
// the launcher what to do with it:
//   - "binary"   → extract to /usr/local (or /opt/rke2)
//   - "images"   → copy to /var/lib/rancher/rke2/agent/images/
//   - "checksum" → used for verification, kept for audit trail
type RKE2Artifact struct {
	Type   string `json:"type"`
	Arch   string `json:"arch"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// AetherOpsEntry describes the aether-ops binary and config in the bundle.
type AetherOpsEntry struct {
	Version        string       `json:"version"`
	Files          []BundleFile `json:"files"`
	OnrampUser     string       `json:"onramp_user,omitempty"`
	OnrampPassword string       `json:"onramp_password,omitempty"`
}

// HelmEntry describes the Helm binary in the bundle.
type HelmEntry struct {
	Version string       `json:"version"`
	Files   []BundleFile `json:"files"`
}

// WheelhouseEntry describes bundled Python wheels plus the normalized
// requirement set they were built from.
type WheelhouseEntry struct {
	Requirements []string     `json:"requirements,omitempty"`
	Files        []BundleFile `json:"files,omitempty"`
}

// OnrampEntry describes the aether-onramp repository bundled as an offline
// payload. The repo is stored as a flat file tree under Path inside the
// bundle. ResolvedSHA is the commit the builder checked out so installs
// are reproducible even when the source Ref is a mutable branch.
//
// TreeSHA256 captures the actual on-disk state shipped, including any
// build-time or tool-applied patches, so consumers can detect content
// drift even when ResolvedSHA is unchanged. Empty on bundles built
// before this field was introduced; consumers must treat empty as
// "unknown" rather than "no patches".
type OnrampEntry struct {
	Repo        string       `json:"repo"`
	Ref         string       `json:"ref,omitempty"`
	ResolvedSHA string       `json:"resolved_sha"`
	TreeSHA256  string       `json:"tree_sha256,omitempty"`
	Path        string       `json:"path"` // directory inside the bundle (e.g. "onramp/aether-onramp")
	Files       []BundleFile `json:"files,omitempty"`
}

// HelmChartsEntry describes a single bundled helm-charts repository.
//
// See OnrampEntry.TreeSHA256 for semantics; the same field on a chart
// entry covers post-clone dependency resolution and any user patches.
type HelmChartsEntry struct {
	Name        string       `json:"name"`
	Repo        string       `json:"repo"`
	Ref         string       `json:"ref,omitempty"`
	ResolvedSHA string       `json:"resolved_sha"`
	TreeSHA256  string       `json:"tree_sha256,omitempty"`
	Path        string       `json:"path"` // directory inside the bundle (e.g. "helm-charts/sdcore-helm-charts")
	Files       []BundleFile `json:"files,omitempty"`
}

// ImagesEntry describes the container images staged in the bundle for
// RKE2's airgap image loader.
type ImagesEntry struct {
	Images []ImageArtifact `json:"images"`
}

// ImageArtifact is one container image saved as an OCI tarball.
type ImageArtifact struct {
	Ref    string `json:"ref"`    // fully qualified image reference
	Digest string `json:"digest"` // sha256:... descriptor digest pinned at pull time
	Path   string `json:"path"`   // bundle-relative tarball path (e.g. "images/ghcr.io_omec-project_amf_rel-1.8.0.tar")
	SHA256 string `json:"sha256"` // hash of the saved tarball
	Size   int64  `json:"size"`
}

// TemplatesEntry describes bundled configuration templates.
type TemplatesEntry struct {
	Files []BundleFile `json:"files"`
}

// BundleFile is a single file entry with its path, hash, and size.
type BundleFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// ComputeTreeSHA256 returns a stable hash that summarizes a set of
// BundleFile entries — `sha256("<path> <sha>\n" * sortedByPath)`.
// Two trees with identical (path, sha256) pairs hash to the same
// digest regardless of input order. Returns the empty string when
// files is empty so callers can use the result directly as a
// JSON-omitempty field.
func ComputeTreeSHA256(files []BundleFile) string {
	if len(files) == 0 {
		return ""
	}
	sorted := make([]BundleFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	h := sha256.New()
	for _, f := range sorted {
		fmt.Fprintf(h, "%s %s\n", f.Path, f.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ErrManifestSchema is returned when a manifest has an unrecognized schema version.
type ErrManifestSchema struct {
	Got  int
	Want int
}

func (e *ErrManifestSchema) Error() string {
	return fmt.Sprintf("unsupported manifest schema version %d (expected %d)", e.Got, e.Want)
}

// Read loads a manifest from a JSON file at the given path.
// Returns an error if the schema version is unrecognized.
func Read(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", path, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
	}

	if m.SchemaVersion != SchemaVersion {
		return nil, &ErrManifestSchema{Got: m.SchemaVersion, Want: SchemaVersion}
	}

	return &m, nil
}

// Write serializes a manifest to a JSON file at the given path.
func Write(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing manifest %s: %w", path, err)
	}

	return nil
}
