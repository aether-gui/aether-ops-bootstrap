package helm

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component installs the Helm binary from the bundle.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{extractDir: extractDir, manifest: manifest}
}

func (c *Component) Name() string { return "helm" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if b.Components.Helm == nil {
		return ""
	}
	return b.Components.Helm.Version
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["helm"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	if desired == "" {
		return components.Plan{NoOp: true}, nil
	}
	if current == desired {
		return components.Plan{NoOp: true}, nil
	}

	if c.manifest == nil {
		return components.Plan{NoOp: true}, nil
	}
	helm := c.manifest.Components.Helm
	if helm == nil || len(helm.Files) == 0 {
		return components.Plan{NoOp: true}, nil
	}

	actions := []components.Action{
		{
			Description: fmt.Sprintf("install Helm %s", desired),
			Fn: func(ctx context.Context) error {
				srcPath := filepath.Join(c.extractDir, helm.Files[0].Path)
				dstPath := "/usr/local/bin/helm"

				src, err := os.Open(srcPath)
				if err != nil {
					return fmt.Errorf("opening helm binary: %w", err)
				}
				defer src.Close()

				dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
				if err != nil {
					return fmt.Errorf("creating %s: %w", dstPath, err)
				}
				_, copyErr := io.Copy(dst, src)
				closeErr := dst.Close()
				if copyErr != nil {
					return copyErr
				}
				if closeErr != nil {
					return closeErr
				}

				log.Printf("  installed %s", dstPath)
				return nil
			},
		},
	}

	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	for _, action := range plan.Actions {
		if err := action.Fn(ctx); err != nil {
			return err
		}
	}
	return nil
}
