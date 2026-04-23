package debs

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/cmdutil"
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
				cmd.Env = nonInteractiveDpkgEnv()
				output, err := cmdutil.Run(ctx, cmd)
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
	return components.ApplyPlan(ctx, c.Name(), plan)
}

// nonInteractiveDpkgEnv returns the environment dpkg should run under so
// maintainer-script prompts do not hang the install. Several bundled
// packages call debconf from their postinst scripts — iptables-persistent
// is the one that bit us in testing ("save existing IPv4/IPv6 rules?"),
// but grub-pc, ufw, kbd-config, and libc6 share the pattern. Without a
// noninteractive frontend debconf opens /dev/tty directly, bypassing the
// io.Writer redirection cmdutil.Run sets up; the install then blocks on
// an invisible prompt forever.
//
// The three variables work together:
//   - DEBIAN_FRONTEND=noninteractive selects debconf's silent frontend.
//   - DEBCONF_NONINTERACTIVE_SEEN=true marks preseed answers as final so
//     the frontend does not escalate back to an interactive fallback.
//   - DEBIAN_PRIORITY=critical tells debconf to skip every question at
//     or below the "critical" priority — i.e. anything that has a
//     defined default answer, which is everything we ship.
//
// os.Environ() is prepended so operator-supplied env (proxies,
// locale settings, custom dpkg options) flows through unchanged.
func nonInteractiveDpkgEnv() []string {
	return append(os.Environ(),
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBIAN_PRIORITY=critical",
	)
}
