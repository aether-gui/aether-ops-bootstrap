package builder

import (
	"runtime"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// BuildManifest constructs a manifest from the spec and build results.
// BundleSHA256 is left empty — it can be set after the archive is created.
func BuildManifest(spec *bundle.Spec, rke2Entry *bundle.RKE2Entry, aetherOpsEntry *bundle.AetherOpsEntry) *bundle.Manifest {
	m := &bundle.Manifest{
		SchemaVersion: bundle.SchemaVersion,
		BundleVersion: spec.BundleVersion,
		BuildInfo: bundle.BuildInfo{
			GoVersion: runtime.Version(),
			Builder:   "build-bundle",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
	}

	if rke2Entry != nil {
		m.Components.RKE2 = rke2Entry
	}
	if aetherOpsEntry != nil {
		m.Components.AetherOps = aetherOpsEntry
	}

	return m
}
