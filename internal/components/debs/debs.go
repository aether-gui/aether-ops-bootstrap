// Package debs installs the bundle's vendored .deb packages by handing
// them to apt-get against a self-contained file:// repository shipped
// inside the bundle.
//
// The previous implementation used `dpkg -i --force-depends …` against
// a curated subset of bundled debs. That left the host's dpkg state
// inconsistent (`apt-get check` regressions, deferred udev / apparmor /
// systemd triggers) and required force-flag laundering of real package
// conflicts (ufw vs iptables-persistent). This implementation lets apt
// own the resolution: write a sources.list pointing at <extractDir>/apt-repo,
// run `apt-get update` then `apt-get install -y --no-install-recommends`
// for the spec's top-level packages, and let apt walk Depends/Conflicts/
// Breaks against the bundled metadata.
//
// Both apt invocations are scoped via `-o Dir::Etc::SourceList=<temp>` and
// `-o Dir::Etc::SourceParts=/dev/null` so apt only consumes the bundle's
// repo for these calls; the host's /etc/apt/sources.list* is never read
// or mutated.
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

// Component installs the bundle's vendored debs via apt-get against a
// local file:// apt repository.
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

// SetSuite sets the detected host suite (e.g., "noble"). Used as the
// suite name in the local sources.list entry; the bundle's apt-repo
// must declare this codename in its dists/ tree.
func (c *Component) SetSuite(suite string) {
	c.suite = suite
}

func (c *Component) Name() string { return "debs" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if b == nil || b.Components.AptRepo == nil || len(b.Components.AptRepo.TopLevel) == 0 {
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
	if desired == "" || current == desired {
		return components.Plan{NoOp: true}, nil
	}
	if c.manifest == nil || c.manifest.Components.AptRepo == nil {
		return components.Plan{NoOp: true}, nil
	}
	ar := c.manifest.Components.AptRepo
	if len(ar.TopLevel) == 0 {
		return components.Plan{NoOp: true}, nil
	}

	repoPath := filepath.Join(c.extractDir, ar.Path)
	sourcesList := filepath.Join(c.extractDir, ".aether-bundle-sources.list")
	suite := c.suite

	actions := []components.Action{
		{
			Description: fmt.Sprintf("write bundle sources.list (suite=%s)", suite),
			Fn: func(ctx context.Context) error {
				return writeSourcesList(sourcesList, repoPath, suite)
			},
		},
		{
			Description: "apt-get update against bundle",
			Fn: func(ctx context.Context) error {
				return runApt(ctx, sourcesList, "update")
			},
		},
		{
			Description: fmt.Sprintf("apt-get install %d top-level packages", len(ar.TopLevel)),
			Fn: func(ctx context.Context) error {
				args := append([]string{
					"install",
					"-y",
					"--no-install-recommends",
					"-o", "APT::Get::AllowDowngrades=true",
				}, ar.TopLevel...)
				return runApt(ctx, sourcesList, args...)
			},
		},
	}
	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}

// writeSourcesList drops a single-line sources.list entry pointing at
// the bundle's local apt repository. `[trusted=yes]` skips signature
// verification — v1 ships the Release file unsigned. The file path is
// passed to apt via `-o Dir::Etc::SourceList=`, so it doesn't have to
// live under /etc/apt/sources.list.d/.
func writeSourcesList(path, repoPath, suite string) error {
	line := fmt.Sprintf("deb [trusted=yes] file://%s %s main\n", repoPath, suite)
	if err := os.WriteFile(path, []byte(line), 0644); err != nil {
		return fmt.Errorf("writing sources.list %s: %w", path, err)
	}
	log.Printf("  wrote %s -> %s", path, line[:len(line)-1])
	return nil
}

// runApt invokes apt-get with the bundle's sources.list as the *only*
// source apt sees for this command. The two `-o Dir::Etc::*` flags work
// together: SourceList replaces /etc/apt/sources.list, SourceParts
// disables /etc/apt/sources.list.d/. The host's apt configuration
// outside of those two paths is preserved.
//
// `update` is invoked the same way `install` is so apt's package-list
// cache is built against the bundle's repo only.
func runApt(ctx context.Context, sourcesList string, args ...string) error {
	full := append([]string{
		"-o", "Dir::Etc::SourceList=" + sourcesList,
		"-o", "Dir::Etc::SourceParts=/dev/null",
	}, args...)

	cmd := exec.CommandContext(ctx, "apt-get", full...)
	cmd.Env = noninteractiveAptEnv()
	out, err := cmdutil.Run(ctx, cmd)
	if err != nil {
		return fmt.Errorf("apt-get %v: %w\n%s", args, err, out)
	}
	return nil
}

// noninteractiveAptEnv returns the environment apt-get should run under
// so debconf prompts don't hang the install. Several bundled packages
// call debconf from their postinst scripts — iptables-persistent is the
// one that bit us in testing ("save existing IPv4/IPv6 rules?"), but
// grub-pc, ufw, kbd-config, and libc6 share the pattern. Without a
// noninteractive frontend debconf opens /dev/tty directly, bypassing
// the io.Writer redirection cmdutil.Run sets up; the install then
// blocks on an invisible prompt forever.
//
// The three variables work together:
//   - DEBIAN_FRONTEND=noninteractive selects debconf's silent frontend.
//   - DEBCONF_NONINTERACTIVE_SEEN=true marks preseed answers as final
//     so the frontend does not escalate back to an interactive fallback.
//   - DEBIAN_PRIORITY=critical tells debconf to skip every question at
//     or below the "critical" priority — i.e. anything that has a
//     defined default answer, which is everything we ship.
//
// os.Environ() is prepended so operator-supplied env (proxies, locale,
// custom apt options) flows through unchanged.
func noninteractiveAptEnv() []string {
	return append(os.Environ(),
		"DEBIAN_FRONTEND=noninteractive",
		"DEBCONF_NONINTERACTIVE_SEEN=true",
		"DEBIAN_PRIORITY=critical",
	)
}
