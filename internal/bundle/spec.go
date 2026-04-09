package bundle

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultRKE2Source is the base URL for RKE2 release artifacts.
const DefaultRKE2Source = "https://github.com/rancher/rke2/releases/download"

// Default GitHub repositories for aether-ops.
const DefaultAetherOpsRepo = "aether-gui/aether-ops"

// Valid image modes for RKE2 artifact packaging.
const (
	ImageModeAllInOne    = "all-in-one"
	ImageModeCoreVariant = "core+variant"
)

// Known Ubuntu suite codenames.
var knownSuites = map[string]bool{
	"jammy":  true, // 22.04
	"noble":  true, // 24.04
	"plucky": true, // 25.04
}

// Known architectures.
var knownArchitectures = map[string]bool{
	"amd64": true,
}

// Spec is the top-level structure of bundle.yaml — the human-edited input
// that drives the bundle builder.
type Spec struct {
	SchemaVersion int            `yaml:"schema_version"`
	BundleVersion string         `yaml:"bundle_version"`
	Ubuntu        UbuntuSpec     `yaml:"ubuntu"`
	Debs          []DebSpec      `yaml:"debs"`
	RKE2          *RKE2Spec      `yaml:"rke2,omitempty"`
	AetherOps     *AetherOpsSpec `yaml:"aether_ops,omitempty"`
	TemplatesDir  string         `yaml:"templates_dir"`
}

// DefaultUbuntuMirror is the default Ubuntu archive URL.
const DefaultUbuntuMirror = "https://archive.ubuntu.com/ubuntu"

// UbuntuSpec declares which Ubuntu suites and architectures to target.
// The builder resolves dependencies per (suite, arch) pair.
type UbuntuSpec struct {
	Suites        []string `yaml:"suites"`
	Architectures []string `yaml:"architectures"`
	Mirror        string   `yaml:"mirror,omitempty"` // Ubuntu archive URL, defaults to archive.ubuntu.com
}

// DebSpec declares a top-level .deb package to include. The builder
// resolves transitive dependencies automatically.
type DebSpec struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version,omitempty"` // optional constraint: ">=2.14", "=1.2.3-1"
}

// RKE2Spec declares the RKE2 version and artifact configuration.
type RKE2Spec struct {
	Version   string   `yaml:"version"`
	Variants  []string `yaml:"variants"`
	ImageMode string   `yaml:"image_mode,omitempty"` // "all-in-one" (default) or "core+variant"
	Source    string   `yaml:"source,omitempty"`     // base URL, defaults to GitHub releases
}

// AetherOpsSpec declares how to acquire the aether-ops binary.
// Three acquisition modes:
//   - Download: version set, no ref/source → download from GitHub releases
//   - Source build: ref set → clone repo at ref, build from source
//   - Local: source set → use a local pre-built binary or release archive
type AetherOpsSpec struct {
	Version        string `yaml:"version"`                   // required: version string (used for ldflags and release URL)
	Source         string `yaml:"source,omitempty"`          // local path to pre-built binary or release tar.gz
	Ref            string `yaml:"ref,omitempty"`             // git ref (tag/branch/SHA) → build from source
	FrontendRef    string `yaml:"frontend_ref,omitempty"`    // override frontend submodule ref (source build only)
	Repo           string `yaml:"repo,omitempty"`            // GitHub owner/name, default: aether-gui/aether-ops
	OnrampUser     string `yaml:"onramp_user,omitempty"`     // OS user for Ansible SSH deployments, default: "aether"
	OnrampPassword string `yaml:"onramp_password,omitempty"` // default: "aether", overridable via AETHER_ONRAMP_PASSWORD env var at install time
}

// ParseSpec reads and parses a bundle.yaml file.
func ParseSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading spec %s: %w", path, err)
	}

	var s Spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing spec %s: %w", path, err)
	}

	applySpecDefaults(&s)
	return &s, nil
}

// applySpecDefaults fills in default values for optional fields.
func applySpecDefaults(s *Spec) {
	if s.Ubuntu.Mirror == "" {
		s.Ubuntu.Mirror = DefaultUbuntuMirror
	}
	s.Ubuntu.Mirror = strings.TrimRight(s.Ubuntu.Mirror, "/")
	if s.RKE2 != nil {
		if s.RKE2.ImageMode == "" {
			s.RKE2.ImageMode = ImageModeAllInOne
		}
		if s.RKE2.Source == "" {
			s.RKE2.Source = DefaultRKE2Source
		}
	}
	if s.AetherOps != nil {
		if s.AetherOps.Repo == "" {
			s.AetherOps.Repo = DefaultAetherOpsRepo
		}
		if s.AetherOps.OnrampUser == "" {
			s.AetherOps.OnrampUser = "aether"
		}
		if s.AetherOps.OnrampPassword == "" {
			s.AetherOps.OnrampPassword = "aether"
		}
	}
}

// ValidateSpec checks a parsed spec for structural correctness.
func ValidateSpec(s *Spec) error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported spec schema_version %d (expected %d)", s.SchemaVersion, SchemaVersion)
	}

	if s.BundleVersion == "" {
		return fmt.Errorf("bundle_version is required")
	}

	if len(s.Ubuntu.Suites) == 0 {
		return fmt.Errorf("ubuntu.suites must contain at least one suite")
	}
	for _, suite := range s.Ubuntu.Suites {
		if !knownSuites[suite] {
			return fmt.Errorf("unrecognized ubuntu suite %q (known: %s)", suite, joinKeys(knownSuites))
		}
	}

	if len(s.Ubuntu.Architectures) == 0 {
		return fmt.Errorf("ubuntu.architectures must contain at least one architecture")
	}
	for _, arch := range s.Ubuntu.Architectures {
		if !knownArchitectures[arch] {
			return fmt.Errorf("unrecognized architecture %q (known: %s)", arch, joinKeys(knownArchitectures))
		}
	}

	for i, d := range s.Debs {
		if d.Name == "" {
			return fmt.Errorf("debs[%d].name is required", i)
		}
	}

	if s.RKE2 != nil {
		if s.RKE2.Version == "" {
			return fmt.Errorf("rke2.version is required when rke2 section is present")
		}
		switch s.RKE2.ImageMode {
		case ImageModeAllInOne, ImageModeCoreVariant:
			// valid
		default:
			return fmt.Errorf("rke2.image_mode must be %q or %q, got %q",
				ImageModeAllInOne, ImageModeCoreVariant, s.RKE2.ImageMode)
		}
		if s.RKE2.ImageMode == ImageModeCoreVariant && len(s.RKE2.Variants) == 0 {
			return fmt.Errorf("rke2.variants must be non-empty when image_mode is %q", ImageModeCoreVariant)
		}
	}

	if s.TemplatesDir == "" {
		return fmt.Errorf("templates_dir is required")
	}

	if s.AetherOps != nil {
		if s.AetherOps.Version == "" {
			return fmt.Errorf("aether_ops.version is required when aether_ops section is present")
		}
		if s.AetherOps.Ref != "" && s.AetherOps.Source != "" {
			return fmt.Errorf("aether_ops.ref and aether_ops.source are mutually exclusive")
		}
		if s.AetherOps.FrontendRef != "" && s.AetherOps.Ref == "" {
			return fmt.Errorf("aether_ops.frontend_ref requires aether_ops.ref (only meaningful for source builds)")
		}
	}

	return nil
}

func joinKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}
