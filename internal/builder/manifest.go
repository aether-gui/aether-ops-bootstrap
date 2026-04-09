package builder

import (
	"runtime"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// BuildManifest constructs a manifest from the spec and build results.
// BundleSHA256 is left empty — it can be set after the archive is created.
func BuildManifest(spec *bundle.Spec, gitSHA string, rke2Entry *bundle.RKE2Entry, helmEntry *bundle.HelmEntry, aetherOpsEntry *bundle.AetherOpsEntry, debEntries []bundle.DebEntry, templatesEntry *bundle.TemplatesEntry) *bundle.Manifest {
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

	if rke2Entry != nil {
		m.Components.RKE2 = rke2Entry
	}
	if helmEntry != nil {
		m.Components.Helm = helmEntry
	}
	if aetherOpsEntry != nil {
		m.Components.AetherOps = aetherOpsEntry
	}
	if len(debEntries) > 0 {
		m.Components.Debs = debEntries
	}
	if templatesEntry != nil {
		m.Components.Templates = templatesEntry
	}

	return m
}
