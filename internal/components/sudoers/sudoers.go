package sudoers

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
	"github.com/aether-gui/aether-ops-bootstrap/internal/cmdutil"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

const sudoersDir = "/etc/sudoers.d"

// Component manages sudoers drop-in files for the service accounts.
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
	if current == desired && desired != "" {
		return components.Plan{NoOp: true}, nil
	}

	sudoersTemplateDir := filepath.Join(c.extractDir, "templates", "sudoers.d")
	entries, err := os.ReadDir(sudoersTemplateDir)
	if os.IsNotExist(err) {
		return components.Plan{NoOp: true}, nil
	}
	if err != nil {
		return components.Plan{}, fmt.Errorf("reading sudoers templates: %w", err)
	}

	onrampUser := c.onrampUser()

	var actions []components.Action
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == ".gitkeep" {
			continue
		}
		entryName := entry.Name()
		actions = append(actions, components.Action{
			Description: fmt.Sprintf("install sudoers drop-in %s", entryName),
			Fn: func(ctx context.Context) error {
				srcPath := filepath.Join(sudoersTemplateDir, entryName)
				raw, err := os.ReadFile(srcPath)
				if err != nil {
					return err
				}

				// Render Go template.
				tmpl, err := template.New(entryName).Parse(string(raw))
				if err != nil {
					return fmt.Errorf("parsing sudoers template %s: %w", entryName, err)
				}
				var buf bytes.Buffer
				if err := tmpl.Execute(&buf, map[string]string{"OnrampUser": onrampUser}); err != nil {
					return fmt.Errorf("rendering sudoers template %s: %w", entryName, err)
				}
				data := buf.Bytes()

				// Write to temp file and validate with visudo before installing.
				tmpFile, err := os.CreateTemp("", "sudoers-*.tmp")
				if err != nil {
					return err
				}
				tmpPath := tmpFile.Name()
				defer os.Remove(tmpPath)

				if _, err := tmpFile.Write(data); err != nil {
					tmpFile.Close()
					return err
				}
				tmpFile.Close()

				cmd := exec.CommandContext(ctx, "visudo", "-c", "-f", tmpPath)
				if output, err := cmdutil.Run(ctx, cmd); err != nil {
					return fmt.Errorf("sudoers validation failed for %s: %w\n%s", entryName, err, output)
				}

				destName := entryName
				if filepath.Ext(destName) == ".tmpl" {
					destName = destName[:len(destName)-5]
				}
				destPath := filepath.Join(sudoersDir, destName)
				if err := os.MkdirAll(sudoersDir, 0755); err != nil {
					return err
				}
				if err := os.WriteFile(destPath, data, 0440); err != nil {
					return err
				}
				log.Printf("  installed %s (validated)", destPath)
				return nil
			},
		})
	}

	if len(actions) == 0 {
		return components.Plan{NoOp: true}, nil
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
