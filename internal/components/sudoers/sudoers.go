package sudoers

import (
	"context"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component manages sudoers drop-in files for the aether-ops service account.
type Component struct{}

func New() *Component {
	return &Component{}
}

func (c *Component) Name() string { return "sudoers" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["sudoers"]; ok {
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
