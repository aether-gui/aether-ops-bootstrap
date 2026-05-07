// Package debs installs the bundle's vendored .deb packages by handing
// them to apt-get against a self-contained file:// repository shipped
// inside the bundle.
//
// The previous implementation used `dpkg -i --force-depends …` against
// a curated subset of bundled debs. That left the host's dpkg state
// inconsistent (`apt-get check` regressions, deferred udev / apparmor /
// systemd triggers) and required force-flag laundering of real package
// conflicts (ufw vs iptables-persistent). This implementation lets apt
// own the resolution: stage the bundle's apt-repo persistently at
// /var/lib/aether-ops/apt-repo, drop a sources.list at
// /etc/apt/sources.list.d/aether-bundle.list, run `apt-get update` then
// `apt-get install -y --no-install-recommends` for the spec's top-level
// packages, and let apt walk Depends/Conflicts/Breaks against the
// bundled metadata.
//
// The persistent staging path is the same pattern used by the onramp
// and helm-charts components — extractDir is a temp tree wiped at the
// end of `Install`, so anything the host needs after bootstrap has to
// land outside it. Staging the apt-repo persistently means the airgap
// operator can `apt install` post-bootstrap (the bundle is the only
// reachable source on an airgap host), and `dpkg --configure -a` and
// kindred recovery tooling have a working repo to pull from.
//
// During the install both apt invocations are still scoped via
// `-o Dir::Etc::SourceList=<temp> -o Dir::Etc::SourceParts=/dev/null`
// so apt resolves only against the bundle's repo, even though the
// persistent sources.list is now in /etc/apt/sources.list.d/. The host
// may carry image-baked archive.ubuntu.com entries that fail to
// resolve in airgap; the scoping prevents those failures from
// poisoning the install. After bootstrap the operator's normal
// `apt update` will fail on the upstream entries (same as before)
// while succeeding on ours, and `apt install` works for anything we
// shipped.
package debs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/cmdutil"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// PersistentRepoDir is where the bundle's apt-repo lives on the host
// after install completes. Mirrors the staging pattern used by
// /var/lib/aether-ops/aether-onramp and /var/lib/aether-ops/helm-charts.
const PersistentRepoDir = "/var/lib/aether-ops/apt-repo"

// SourcesListPath is where the persistent sources.list entry lives so
// post-bootstrap operator-driven `apt install` can resolve from the
// bundle's repo. The file is written/overwritten on every install so
// the line stays in sync with PersistentRepoDir.
const SourcesListPath = "/etc/apt/sources.list.d/aether-bundle.list"

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

	stagedRepo := filepath.Join(c.extractDir, ar.Path)
	suite := c.suite

	actions := []components.Action{
		{
			Description: fmt.Sprintf("stage apt-repo to %s", PersistentRepoDir),
			Fn: func(ctx context.Context) error {
				return stageRepo(stagedRepo, PersistentRepoDir)
			},
		},
		{
			Description: fmt.Sprintf("write %s (suite=%s)", SourcesListPath, suite),
			Fn: func(ctx context.Context) error {
				return writeSourcesList(SourcesListPath, PersistentRepoDir, suite)
			},
		},
		{
			Description: "apt-get update against bundle",
			Fn: func(ctx context.Context) error {
				return runApt(ctx, SourcesListPath, "update")
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
				return runApt(ctx, SourcesListPath, args...)
			},
		},
	}
	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}

// writeSourcesList drops a sources.list entry pointing at the bundle's
// local apt repository. `[trusted=yes]` skips signature verification —
// v1 ships the Release file unsigned.
//
// The file is written with a header explaining where it came from and
// how to disable it, since /etc/apt/sources.list.d/ is system config
// the operator may inspect. The parent dir is created if missing
// (it's already there on every Debian/Ubuntu host, but cheap insurance
// for tests against fresh tempdirs).
func writeSourcesList(path, repoPath, suite string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating %s parent: %w", path, err)
	}
	contents := fmt.Sprintf(`# Aether-ops bundle local apt repository.
# Generated by aether-ops-bootstrap; safe to leave in place. The bundle
# itself is staged at %s. Disable by deleting
# this file or commenting the deb line below.
deb [trusted=yes] file://%s %s main
`, repoPath, repoPath, suite)
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		return fmt.Errorf("writing sources.list %s: %w", path, err)
	}
	log.Printf("  wrote %s", path)
	return nil
}

// stageRepo moves the bundle's apt-repo from the launcher's temp
// extractDir to a persistent location at /var/lib/aether-ops/apt-repo.
// extractDir is removed at the end of `Install`, so the apt-repo (and
// every .deb in it) would otherwise vanish — leaving the operator's
// future `apt install` calls without a working source on an airgap host.
//
// Tries os.Rename first (fast: a single inode-table update on the same
// filesystem), and falls back to a recursive copy on EXDEV — common
// when /tmp is a tmpfs mount distinct from /var. Any pre-existing
// destination is removed first so re-runs (e.g. `--force` reinstall
// or upgrade with a different bundle version) replace the old contents
// atomically rather than mixing two bundle versions.
func stageRepo(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("locating staged apt-repo at %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("expected apt-repo at %s to be a directory, got %s", src, info.Mode())
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("creating %s parent: %w", dst, err)
	}

	// Wipe any prior install's repo so we don't mix versions.
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("removing existing %s: %w", dst, err)
	}

	if err := os.Rename(src, dst); err == nil {
		log.Printf("  moved %s -> %s", src, dst)
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("moving %s -> %s: %w", src, dst, err)
	}

	// Cross-device rename — fall back to recursive copy.
	log.Printf("  copying %s -> %s (cross-device fallback)", src, dst)
	if err := copyDir(src, dst); err != nil {
		return fmt.Errorf("copying %s -> %s: %w", src, dst, err)
	}
	// Best-effort cleanup of the source so extractDir's later removal
	// doesn't have to walk the now-redundant tree. A failure here is a
	// warning, not fatal: the launcher's defer os.RemoveAll(extractDir)
	// will still clean it up.
	if err := os.RemoveAll(src); err != nil {
		log.Printf("  warning: cleanup of staged repo at %s failed: %v", src, err)
	}
	return nil
}

// copyDir recursively copies src into dst, preserving file modes for
// regular files. Mirrors the helper in internal/components/onramp; the
// bundled apt-repo is a clean tree of regular files (.deb plus
// generated metadata), so symlinks and special files do not appear.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
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
