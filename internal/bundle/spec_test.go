package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSpec(t *testing.T) {
	// Parse the real bundle.yaml from the repo root.
	s, err := ParseSpec("../../bundle.yaml")
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}

	if s.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", s.SchemaVersion)
	}
	if s.BundleVersion != "0.0.0-dev" {
		t.Errorf("BundleVersion = %q, want %q", s.BundleVersion, "0.0.0-dev")
	}

	// Ubuntu targets.
	if len(s.Ubuntu.Suites) != 2 {
		t.Fatalf("len(Suites) = %d, want 2", len(s.Ubuntu.Suites))
	}
	if s.Ubuntu.Suites[0] != "jammy" {
		t.Errorf("Suites[0] = %q, want %q", s.Ubuntu.Suites[0], "jammy")
	}
	if len(s.Ubuntu.Architectures) == 0 {
		t.Fatal("len(Architectures) = 0, want at least 1")
	}

	// Debs.
	if len(s.Debs) < 3 {
		t.Fatalf("len(Debs) = %d, want at least 3", len(s.Debs))
	}
	debNames := map[string]bool{}
	for _, d := range s.Debs {
		debNames[d.Name] = true
	}
	for _, want := range []string{"ansible", "git", "make"} {
		if !debNames[want] {
			t.Errorf("missing deb %q", want)
		}
	}
	// Check version constraint on ansible.
	if s.Debs[0].Name != "ansible" || s.Debs[0].Version != ">=2.14" {
		t.Errorf("ansible version = %q, want %q", s.Debs[0].Version, ">=2.14")
	}

	// RKE2.
	if s.RKE2 == nil {
		t.Fatal("RKE2 is nil")
	}
	if s.RKE2.Version != "v1.33.1+rke2r1" {
		t.Errorf("RKE2.Version = %q, want %q", s.RKE2.Version, "v1.33.1+rke2r1")
	}
	if s.RKE2.ImageMode != ImageModeAllInOne {
		t.Errorf("RKE2.ImageMode = %q, want %q", s.RKE2.ImageMode, ImageModeAllInOne)
	}
	if s.RKE2.Source != DefaultRKE2Source {
		t.Errorf("RKE2.Source = %q, want default %q", s.RKE2.Source, DefaultRKE2Source)
	}
	if len(s.RKE2.Variants) != 1 || s.RKE2.Variants[0] != "canal" {
		t.Errorf("RKE2.Variants = %v, want [canal]", s.RKE2.Variants)
	}

	// AetherOps.
	if s.AetherOps == nil {
		t.Fatal("AetherOps is nil")
	}
	if s.AetherOps.Version != "v0.0.0-dev" {
		t.Errorf("AetherOps.Version = %q, want %q", s.AetherOps.Version, "v0.0.0-dev")
	}
	if s.AetherOps.Source != "./build/aether-ops" {
		t.Errorf("AetherOps.Source = %q, want %q", s.AetherOps.Source, "./build/aether-ops")
	}
	if s.AetherOps.Repo != DefaultAetherOpsRepo {
		t.Errorf("AetherOps.Repo = %q, want default %q", s.AetherOps.Repo, DefaultAetherOpsRepo)
	}
	// Templates.
	if s.TemplatesDir != "./templates" {
		t.Errorf("TemplatesDir = %q, want %q", s.TemplatesDir, "./templates")
	}
}

func TestParseSpecDefaults(t *testing.T) {
	yaml := `
schema_version: 1
bundle_version: "1.0.0"
ubuntu:
  suites: [jammy]
  architectures: [amd64]
rke2:
  version: "v1.33.1+rke2r1"
  variants: [canal]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := ParseSpec(path)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}

	// Defaults should be applied.
	if s.RKE2.ImageMode != ImageModeAllInOne {
		t.Errorf("ImageMode default = %q, want %q", s.RKE2.ImageMode, ImageModeAllInOne)
	}
	if s.RKE2.Source != DefaultRKE2Source {
		t.Errorf("Source default = %q, want %q", s.RKE2.Source, DefaultRKE2Source)
	}
}

func TestParseSpecMissingFile(t *testing.T) {
	_, err := ParseSpec("/nonexistent/bundle.yaml")
	if err == nil {
		t.Fatal("ParseSpec should fail on missing file")
	}
}

func TestParseSpecInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(path, []byte("{{not yaml"), 0644)

	_, err := ParseSpec(path)
	if err == nil {
		t.Fatal("ParseSpec should fail on invalid YAML")
	}
}

func TestValidateSpecValid(t *testing.T) {
	s, err := ParseSpec("../../bundle.yaml")
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	if err := ValidateSpec(s); err != nil {
		t.Fatalf("ValidateSpec: %v", err)
	}
}

func TestValidateSpecBadSchemaVersion(t *testing.T) {
	s := &Spec{SchemaVersion: 99, BundleVersion: "1.0.0"}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject bad schema version")
	}
}

func TestValidateSpecMissingBundleVersion(t *testing.T) {
	s := &Spec{SchemaVersion: 1}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject missing bundle_version")
	}
}

func TestValidateSpecBadSuite(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"bionic"}, Architectures: []string{"amd64"}},
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject unrecognized suite")
	}
}

func TestValidateSpecBadArch(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"s390x"}},
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject unrecognized architecture")
	}
}

func TestValidateSpecBadImageMode(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		RKE2:          &RKE2Spec{Version: "v1.33.1+rke2r1", ImageMode: "invalid"},
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject invalid image_mode")
	}
}

func TestValidateSpecCoreVariantRequiresVariants(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		RKE2:          &RKE2Spec{Version: "v1.33.1+rke2r1", ImageMode: ImageModeCoreVariant, Variants: nil},
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject core+variant with empty variants")
	}
}

func TestValidateSpecRKE2MissingVersion(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		RKE2:          &RKE2Spec{ImageMode: ImageModeAllInOne},
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject RKE2 without version")
	}
}

func TestValidateSpecEmptyDebName(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		Debs:          []DebSpec{{Name: ""}},
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject deb with empty name")
	}
}

func TestValidateSpecNoDebs(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		TemplatesDir:  "./templates",
		Debs:          nil,
	}
	// No debs is valid — some bundles might only have RKE2.
	if err := ValidateSpec(s); err != nil {
		t.Fatalf("empty debs should be valid: %v", err)
	}
}

func TestValidateSpecMissingTemplatesDir(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		TemplatesDir:  "",
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject empty templates_dir")
	}
}

func TestValidateSpecAetherOpsVersionOnly(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		TemplatesDir:  "./templates",
		AetherOps:     &AetherOpsSpec{Version: "v1.0.0"},
	}
	if err := ValidateSpec(s); err != nil {
		t.Fatalf("version-only (download mode) should be valid: %v", err)
	}
}

func TestValidateSpecAetherOpsRefWithVersion(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		TemplatesDir:  "./templates",
		AetherOps:     &AetherOpsSpec{Version: "v0.0.0-dev", Ref: "main"},
	}
	if err := ValidateSpec(s); err != nil {
		t.Fatalf("ref+version (source mode) should be valid: %v", err)
	}
}

func TestValidateSpecAetherOpsMissingVersion(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		TemplatesDir:  "./templates",
		AetherOps:     &AetherOpsSpec{Ref: "main"},
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject aether_ops without version")
	}
}

func TestValidateSpecAetherOpsRefAndSourceMutuallyExclusive(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		TemplatesDir:  "./templates",
		AetherOps:     &AetherOpsSpec{Version: "v1.0.0", Ref: "main", Source: "./binary"},
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject ref + source together")
	}
}

func TestValidateSpecAetherOpsFrontendRefWithoutRef(t *testing.T) {
	s := &Spec{
		SchemaVersion: 1,
		BundleVersion: "1.0.0",
		Ubuntu:        UbuntuSpec{Suites: []string{"jammy"}, Architectures: []string{"amd64"}},
		TemplatesDir:  "./templates",
		AetherOps:     &AetherOpsSpec{Version: "v1.0.0", FrontendRef: "v2.0.0"},
	}
	if err := ValidateSpec(s); err == nil {
		t.Fatal("should reject frontend_ref without ref")
	}
}
