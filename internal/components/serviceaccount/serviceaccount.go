package serviceaccount

import (
	"context"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component creates the aether-ops service account and group.
type Component struct{}

func New() *Component {
	return &Component{}
}

func (c *Component) Name() string { return "service_account" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["service_account"]; ok {
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
