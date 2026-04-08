package bundle

import (
	"encoding/json"
	"fmt"
	"os"
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
	Debs      []DebEntry      `json:"debs,omitempty"`
	RKE2      *RKE2Entry      `json:"rke2,omitempty"`
	AetherOps *AetherOpsEntry `json:"aether_ops,omitempty"`
	Templates *TemplatesEntry `json:"templates,omitempty"`
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
	Version string       `json:"version"`
	Files   []BundleFile `json:"files"`
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

// Read loads a manifest from a JSON file at the given path.
func Read(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", path, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
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
