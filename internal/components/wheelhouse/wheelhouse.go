package wheelhouse

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

const (
	wheelhouseRoot = "/var/lib/aether-ops/wheelhouse"
	pipConfigPath  = "/etc/pip.conf"
)

// Component stages bundled Python wheels and writes a target-side pip
// config that forces downstream installs to use only the local wheelhouse.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{extractDir: extractDir, manifest: manifest}
}

func (c *Component) Name() string { return "wheelhouse" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if b == nil || b.Components.Wheelhouse == nil || len(b.Components.Wheelhouse.Files) == 0 {
		return ""
	}
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["wheelhouse"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	if desired == "" || current == desired || c.manifest == nil || c.manifest.Components.Wheelhouse == nil {
		return components.Plan{NoOp: true}, nil
	}

	entry := c.manifest.Components.Wheelhouse
	actions := []components.Action{
		{
			Description: fmt.Sprintf("install offline wheelhouse (%d files)", len(entry.Files)),
			Fn: func(ctx context.Context) error {
				return c.installWheelhouse(entry)
			},
		},
		{
			Description: "write offline pip configuration",
			Fn: func(ctx context.Context) error {
				return writePipConfig()
			},
		},
	}
	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}

func (c *Component) installWheelhouse(entry *bundle.WheelhouseEntry) error {
	if err := os.RemoveAll(wheelhouseRoot); err != nil {
		return fmt.Errorf("removing existing wheelhouse: %w", err)
	}
	if err := os.MkdirAll(wheelhouseRoot, 0o755); err != nil {
		return fmt.Errorf("creating wheelhouse root: %w", err)
	}

	for _, f := range entry.Files {
		src := filepath.Join(c.extractDir, f.Path)
		dst := filepath.Join(wheelhouseRoot, filepath.Base(f.Path))
		if filepath.Base(f.Path) == "requirements.txt" {
			dst = filepath.Join(wheelhouseRoot, "requirements.txt")
		}
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copying wheelhouse file %s: %w", f.Path, err)
		}
	}
	log.Printf("  installed %s", wheelhouseRoot)
	return nil
}

func writePipConfig() error {
	content := []byte("[global]\nno-index = true\nfind-links = " + wheelhouseRoot + "\n")
	if err := os.WriteFile(pipConfigPath, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", pipConfigPath, err)
	}
	log.Printf("  wrote %s", pipConfigPath)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
