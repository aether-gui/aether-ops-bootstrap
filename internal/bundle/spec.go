package bundle

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultRKE2Source is the base URL for RKE2 release artifacts.
const DefaultRKE2Source = "https://github.com/rancher/rke2/releases/download"

// Valid image modes for RKE2 artifact packaging.
const (
	ImageModeAllInOne    = "all-in-one"
	ImageModeCoreVariant = "core+variant"
)

// Known Ubuntu suite codenames.
var knownSuites = map[string]bool{
	"jammy": true, // 22.04
	"noble": true, // 24.04
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

// UbuntuSpec declares which Ubuntu suites and architectures to target.
// The builder resolves dependencies per (suite, arch) pair.
type UbuntuSpec struct {
	Suites        []string `yaml:"suites"`
	Architectures []string `yaml:"architectures"`
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
	Source    string   `yaml:"source,omitempty"`      // base URL, defaults to GitHub releases
}

// AetherOpsSpec declares the aether-ops version and artifact source.
type AetherOpsSpec struct {
	Version string `yaml:"version"`
	Source  string `yaml:"source"`
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
	if s.RKE2 != nil {
		if s.RKE2.ImageMode == "" {
			s.RKE2.ImageMode = ImageModeAllInOne
		}
		if s.RKE2.Source == "" {
			s.RKE2.Source = DefaultRKE2Source
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

	if s.AetherOps != nil {
		if s.AetherOps.Version == "" {
			return fmt.Errorf("aether_ops.version is required when aether_ops section is present")
		}
		if s.AetherOps.Source == "" {
			return fmt.Errorf("aether_ops.source is required when aether_ops section is present")
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
