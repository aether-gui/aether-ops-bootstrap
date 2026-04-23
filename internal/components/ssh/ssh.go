package ssh

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

const sshdDropInDir = "/etc/ssh/sshd_config.d"

// Component configures sshd with drop-in snippets from the bundle templates.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

func New(extractDir string) *Component {
	return &Component{extractDir: extractDir}
}

// SetManifest allows the launcher to pass the manifest for reading
// onramp user configuration.
func (c *Component) SetManifest(m *bundle.Manifest) {
	c.manifest = m
}

func (c *Component) Name() string { return "ssh" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["ssh"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	if current == desired && desired != "" {
		return components.Plan{NoOp: true}, nil
	}

	srcPath := filepath.Join(c.extractDir, "templates", "sshd_config.d", "01-aether-password-auth.conf")
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return components.Plan{NoOp: true}, nil
	}

	onrampUser := c.onrampUser()

	actions := []components.Action{
		{
			Description: "drop sshd password auth config",
			Fn: func(ctx context.Context) error {
				if err := os.MkdirAll(sshdDropInDir, 0755); err != nil {
					return err
				}
				raw, err := os.ReadFile(srcPath)
				if err != nil {
					return fmt.Errorf("reading sshd template: %w", err)
				}
				tmpl, err := template.New("sshd").Parse(string(raw))
				if err != nil {
					return fmt.Errorf("parsing sshd template: %w", err)
				}
				var buf bytes.Buffer
				if err := tmpl.Execute(&buf, map[string]string{"OnrampUser": onrampUser}); err != nil {
					return fmt.Errorf("rendering sshd template: %w", err)
				}
				destPath := filepath.Join(sshdDropInDir, "01-aether-password-auth.conf")
				if err := os.WriteFile(destPath, buf.Bytes(), 0644); err != nil {
					return fmt.Errorf("writing sshd drop-in: %w", err)
				}
				log.Printf("  wrote %s", destPath)

				// Restart sshd to pick up config changes.
				for _, unit := range []string{"ssh", "sshd"} {
					cmd := exec.CommandContext(ctx, "systemctl", "restart", unit)
					if err := cmd.Run(); err == nil {
						log.Printf("  restarted %s", unit)
						return nil
					}
				}
				log.Printf("  sshd restart skipped (service not found)")
				return nil
			},
		},
	}

	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}

func (c *Component) onrampUser() string {
	if c.manifest != nil && c.manifest.Components.AetherOps != nil && c.manifest.Components.AetherOps.OnrampUser != "" {
		return c.manifest.Components.AetherOps.OnrampUser
	}
	return "aether"
}
