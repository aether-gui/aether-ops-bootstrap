package debs

import (
	"context"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component installs vendored .deb packages via dpkg.
type Component struct{}

// New creates a new debs component.
func New() *Component {
	return &Component{}
}

func (c *Component) Name() string { return "debs" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if len(b.Components.Debs) == 0 {
		return ""
	}
	// The "version" of the debs component is the bundle version,
	// since the set of debs changes with each bundle.
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["debs"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	return components.Plan{}, components.ErrNotImplemented
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ErrNotImplemented
}
