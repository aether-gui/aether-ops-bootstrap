package ssh

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"text/template"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
	"github.com/aether-gui/aether-ops-bootstrap/internal/systemd"
)

const sshdDropInDir = "/etc/ssh/sshd_config.d"

// Component configures sshd with drop-in snippets from the bundle templates.
type Component struct {
	extractDir string
}

func New(extractDir string) *Component {
	return &Component{extractDir: extractDir}
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
		// No SSH template in bundle — skip.
		return components.Plan{NoOp: true}, nil
	}

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
				// Render Go template.
				tmpl, err := template.New("sshd").Parse(string(raw))
				if err != nil {
					return fmt.Errorf("parsing sshd template: %w", err)
				}
				var buf bytes.Buffer
				if err := tmpl.Execute(&buf, map[string]string{"OnrampUser": "aether"}); err != nil {
					return fmt.Errorf("rendering sshd template: %w", err)
				}
				data := buf.Bytes()
				destPath := filepath.Join(sshdDropInDir, "01-aether-password-auth.conf")
				if err := os.WriteFile(destPath, data, 0644); err != nil {
					return fmt.Errorf("writing sshd drop-in: %w", err)
				}
				log.Printf("  wrote %s", destPath)

				// Restart sshd.
				mgr := &systemd.SystemctlManager{}
				// Try both "ssh" and "sshd" unit names (Ubuntu uses "ssh").
				for _, unit := range []string{"ssh", "sshd"} {
					if err := mgr.Start(ctx, unit); err == nil {
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
	for _, action := range plan.Actions {
		if err := action.Fn(ctx); err != nil {
			return err
		}
	}
	return nil
}
