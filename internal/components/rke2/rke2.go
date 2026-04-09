package rke2

import (
	"context"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component installs RKE2 from vendored tarballs and manages its systemd service.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{extractDir: extractDir, manifest: manifest}
}

func (c *Component) Name() string { return "rke2" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if b.Components.RKE2 == nil {
		return ""
	}
	return b.Components.RKE2.Version
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["rke2"]; ok {
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
