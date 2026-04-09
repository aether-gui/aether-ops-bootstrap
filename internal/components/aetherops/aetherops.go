package aetherops

import (
	"context"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component installs the aether-ops daemon binary, systemd unit, and config.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{extractDir: extractDir, manifest: manifest}
}

func (c *Component) Name() string { return "aether_ops" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if b.Components.AetherOps == nil {
		return ""
	}
	return b.Components.AetherOps.Version
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["aether_ops"]; ok {
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
