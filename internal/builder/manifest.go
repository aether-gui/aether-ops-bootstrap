package builder

import (
	"runtime"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// ManifestInputs bundles the results of every builder step so BuildManifest
// has a single structured argument. Nil / empty fields are omitted.
type ManifestInputs struct {
	RKE2       *bundle.RKE2Entry
	Helm       *bundle.HelmEntry
	AetherOps  *bundle.AetherOpsEntry
	Debs       []bundle.DebEntry
	Templates  *bundle.TemplatesEntry
	Onramp     *bundle.OnrampEntry
	HelmCharts []bundle.HelmChartsEntry
	Images     *bundle.ImagesEntry
}

// BuildManifest constructs a manifest from the spec and the builder's
// staged outputs. BundleSHA256 is left empty — it can be set after the
// archive is created.
func BuildManifest(spec *bundle.Spec, gitSHA string, in ManifestInputs) *bundle.Manifest {
	m := &bundle.Manifest{
		SchemaVersion: bundle.SchemaVersion,
		BundleVersion: spec.BundleVersion,
		BuildInfo: bundle.BuildInfo{
			GoVersion: runtime.Version(),
			GitSHA:    gitSHA,
			Builder:   "build-bundle",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}

	if in.RKE2 != nil {
		m.Components.RKE2 = in.RKE2
	}
	if in.Helm != nil {
		m.Components.Helm = in.Helm
	}
	if in.AetherOps != nil {
		m.Components.AetherOps = in.AetherOps
	}
	if len(in.Debs) > 0 {
		m.Components.Debs = in.Debs
	}
	if in.Templates != nil {
		m.Components.Templates = in.Templates
	}
	if in.Onramp != nil {
		m.Components.Onramp = in.Onramp
	}
	if len(in.HelmCharts) > 0 {
		m.Components.HelmCharts = in.HelmCharts
	}
	if in.Images != nil && len(in.Images.Images) > 0 {
		m.Components.Images = in.Images
	}

	return m
}
