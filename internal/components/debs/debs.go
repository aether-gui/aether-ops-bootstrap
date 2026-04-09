package debs

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// Component installs vendored .deb packages via dpkg.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
	suite      string // detected host suite, set before Plan
}

// New creates a new debs component.
func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{
		extractDir: extractDir,
		manifest:   manifest,
	}
}

// SetSuite sets the detected host suite (e.g., "noble") for filtering debs.
func (c *Component) SetSuite(suite string) {
	c.suite = suite
}

func (c *Component) Name() string { return "debs" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if len(b.Components.Debs) == 0 {
		return ""
	}
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["debs"]; ok {
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

	// Filter debs for the host suite and amd64 arch.
	var debsToInstall []bundle.DebEntry
	for _, d := range c.manifest.Components.Debs {
		if d.Suite == c.suite && (d.Arch == "amd64" || d.Arch == "all") {
			debsToInstall = append(debsToInstall, d)
		}
	}

	if len(debsToInstall) == 0 {
		return components.Plan{NoOp: true}, nil
	}

	// Collect all deb paths for a single dpkg call.
	// dpkg handles dependency ordering within a single invocation.
	var debPaths []string
	for _, d := range debsToInstall {
		debPaths = append(debPaths, filepath.Join(c.extractDir, d.Filename))
	}

	actions := []components.Action{
		{
			Description: fmt.Sprintf("install %d .deb packages", len(debPaths)),
			Fn: func(ctx context.Context) error {
				log.Printf("  installing %d packages via dpkg", len(debPaths))
				// Force flags handle airgap scenarios where bundled packages
				// may be slightly older than what the host has via security updates.
				args := append([]string{"-i", "--force-depends", "--force-downgrade", "--force-breaks", "--force-overwrite"}, debPaths...)
				cmd := exec.CommandContext(ctx, "dpkg", args...)
				output, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("dpkg -i: %w\n%s", err, output)
				}
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
