package debs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	var matching []bundle.DebEntry
	for _, d := range c.manifest.Components.Debs {
		if d.Suite == c.suite && (d.Arch == "amd64" || d.Arch == "all") {
			matching = append(matching, d)
		}
	}

	if len(matching) == 0 {
		return components.Plan{NoOp: true}, nil
	}

	// Skip packages already installed on the host. The bundle's
	// resolver pulls transitive system libs (libpam-systemd,
	// libnss-systemd, …) that ship on every vanilla Ubuntu base
	// image. dpkg-installing newer point-release versions of those
	// libs over the image-baked versions wedges apt: sibling
	// systemd-family packages (systemd-resolved, systemd-timesyncd)
	// aren't in the bundle, so they stay at the image's older point
	// release and end up with mismatched ABI vs the just-installed
	// libsystemd0 / libsystemd-shared. Future `apt install` then
	// fails on unmet dependencies.
	//
	// Leaving image-baked packages alone lets the operator run a
	// normal `apt full-upgrade` whenever they're online and have
	// apt resolve everything coherently against current pockets.
	installed, err := installedPackages(context.Background())
	if err != nil {
		log.Printf("  warning: dpkg-query failed (%v); installing all bundled debs", err)
		installed = nil
	}
	var debsToInstall []bundle.DebEntry
	skipped := 0
	for _, d := range matching {
		if installed[d.Name] {
			skipped++
			continue
		}
		debsToInstall = append(debsToInstall, d)
	}
	if skipped > 0 {
		log.Printf("  skipping %d already-installed packages, %d to install", skipped, len(debsToInstall))
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

// installedPackages queries dpkg for packages currently in
// "install ok installed" state on the host. A dpkg-query failure
// (e.g. on a non-Debian test host) returns an error; callers fall
// back to installing every bundled deb.
func installedPackages(ctx context.Context) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Package}\t${Status}\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("dpkg-query: %w", err)
	}
	return parseDpkgQuery(out), nil
}

// parseDpkgQuery extracts the names of packages whose Status third
// word is "installed" (i.e. fully installed, not deconfigured /
// half-installed / config-files-only). dpkg's Status field is three
// space-separated tokens: <want> <eflag> <status> — only the third
// matters for "is this package present and usable on the host".
func parseDpkgQuery(out []byte) map[string]bool {
	installed := make(map[string]bool)
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		line := s.Text()
		i := strings.IndexByte(line, '\t')
		if i < 0 {
			continue
		}
		name := line[:i]
		fields := strings.Fields(line[i+1:])
		if len(fields) >= 3 && fields[2] == "installed" {
			installed[name] = true
		}
	}
	return installed
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
