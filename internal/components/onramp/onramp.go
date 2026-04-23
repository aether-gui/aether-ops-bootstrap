// Package onramp installs the bundled aether-onramp repository and any
// helm chart repositories into the target filesystem so the aether-ops
// service can run Ansible playbooks and helm installs fully offline.
package onramp

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/installctx"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

const (
	// onrampRoot is where aether-ops expects to find its Ansible toolchain.
	onrampRoot = "/var/lib/aether-ops/aether-onramp"

	// helmChartsRoot holds the bundled helm chart repositories as
	// siblings of the onramp directory. Each entry in the manifest is
	// placed under this root with its declared Name.
	helmChartsRoot = "/var/lib/aether-ops/helm-charts"
)

// Component installs the aether-onramp repo plus any helm chart repos
// bundled alongside it. Versioning uses the composite ResolvedSHA(s)
// recorded in the manifest — a change in either the onramp SHA or any
// chart SHA triggers a re-extract.
type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{extractDir: extractDir, manifest: manifest}
}

func (c *Component) Name() string { return "onramp" }

// DesiredVersion returns a composite identifier derived from the onramp
// SHA and each helm chart SHA. A single-string version keeps the state
// schema simple; any change in any underlying repo invalidates the
// composite and causes a re-apply.
func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	return composeVersion(b)
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["onramp"]; ok {
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

	var actions []components.Action

	if c.manifest.Components.Onramp != nil {
		entry := c.manifest.Components.Onramp
		actions = append(actions, components.Action{
			Description: fmt.Sprintf("install aether-onramp (%s)", shortSHA(entry.ResolvedSHA)),
			Fn: func(ctx context.Context) error {
				return c.extractRepo(entry.Path, onrampRoot)
			},
		})
	}

	for _, hc := range c.manifest.Components.HelmCharts {
		dest := filepath.Join(helmChartsRoot, hc.Name)
		actions = append(actions, components.Action{
			Description: fmt.Sprintf("install helm charts %q (%s)", hc.Name, shortSHA(hc.ResolvedSHA)),
			Fn: func(ctx context.Context) error {
				return c.extractRepo(hc.Path, dest)
			},
		})
	}

	if len(actions) == 0 {
		return components.Plan{NoOp: true}, nil
	}

	// The aether-ops daemon drives ansible-playbook against onramp's
	// bundled hosts.ini; Ansible needs a working credential on node1
	// before the daemon's first inventory sync. Stamp the resolved
	// onramp user and password into the inventory so the daemon does
	// not have to guess.
	onrampUser := "aether"
	if c.manifest != nil && c.manifest.Components.AetherOps != nil && c.manifest.Components.AetherOps.OnrampUser != "" {
		onrampUser = c.manifest.Components.AetherOps.OnrampUser
	}
	actions = append(actions, components.Action{
		Description: "set onramp ansible credentials in hosts.ini",
		Fn: func(ctx context.Context) error {
			password := installctx.OnrampPasswordFromContext(ctx)
			if password == "" {
				return fmt.Errorf("onramp password missing on context; launcher should resolve it before Apply")
			}
			return setInventoryCredentials(filepath.Join(onrampRoot, "hosts.ini"), onrampUser, password)
		},
	})

	// Ownership is applied after all content is in place so partial
	// failures don't leave a half-chowned tree.
	actions = append(actions, components.Action{
		Description: "set ownership to aether-ops",
		Fn: func(ctx context.Context) error {
			if err := chownTree(onrampRoot, "aether-ops", "aether-ops"); err != nil {
				return err
			}
			return chownTree(helmChartsRoot, "aether-ops", "aether-ops")
		},
	})

	return components.Plan{Actions: actions}, nil
}

// inventoryNodeLine matches an uncommented ansible hosts.ini node line,
// e.g. "node1 ansible_host=127.0.0.1 ansible_user=aether". The capture
// anchors on the presence of "ansible_host=" so we do not mistakenly
// rewrite group headers or unrelated variables.
var inventoryNodeLine = regexp.MustCompile(`^[ \t]*[A-Za-z0-9_.-]+[ \t]+[^\n#]*\bansible_host=`)

// setInventoryCredentials rewrites every uncommented node line in the
// onramp hosts.ini so it carries `ansible_user=<user> ansible_password=<pw>`.
// Existing values for those two keys are replaced; any other tokens on the
// line (ansible_host, ansible_port, custom vars) are preserved verbatim.
//
// Idempotent — running twice with the same user/password yields an
// identical file. Callers invoke this every install/upgrade/repair so
// password rotations at the launcher layer propagate into the inventory.
func setInventoryCredentials(path, onrampUser, password string) error {
	// Defense-in-depth: the launcher validates the password on resolve,
	// but a rogue callpath (tests, future direct API) could stuff a
	// newline-bearing value onto the context. Rewriting hosts.ini with
	// such a value would split the node record across lines.
	if err := installctx.ValidateOnrampPassword(password); err != nil {
		return fmt.Errorf("onramp password: %w", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	lines := strings.Split(string(content), "\n")
	changed := false
	for i, line := range lines {
		if !inventoryNodeLine.MatchString(line) {
			continue
		}
		rewritten := setInventoryToken(line, "ansible_user", onrampUser)
		rewritten = setInventoryToken(rewritten, "ansible_password", password)
		if rewritten != line {
			lines[i] = rewritten
			changed = true
		}
	}
	if !changed {
		log.Printf("  %s already has ansible credentials set", path)
		return nil
	}

	// Mode 0640 is intentional: the daemon runs as aether-ops and must
	// read this file; nothing else on the host should.
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0640); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	log.Printf("  stamped ansible credentials into %s", path)
	return nil
}

// setInventoryToken returns line with key=value either replacing the
// existing key=... occurrence or appended to the end. The value is
// shell-quoted defensively so an onramp password containing whitespace
// or '#' does not truncate the line or create a comment.
func setInventoryToken(line, key, value string) string {
	quoted := inventoryQuote(value)
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `=(?:'[^']*'|"[^"]*"|\S+)`)
	replacement := key + "=" + quoted
	if re.MatchString(line) {
		return re.ReplaceAllString(line, replacement)
	}
	return strings.TrimRight(line, " \t") + " " + replacement
}

// inventoryQuote returns value wrapped in single quotes when it contains
// whitespace, '#', or other characters ansible's ini parser treats as
// separators. Inner single quotes are escaped using the standard sh
// close-quote + literal-quote + open-quote sequence: foo'bar becomes
// 'foo'\”bar'.
func inventoryQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t#\"'\\") {
		return value
	}
	escaped := strings.ReplaceAll(value, "'", `'\''`)
	return "'" + escaped + "'"
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}

// extractRepo copies a bundled repo directory from the extracted bundle
// into destDir. Any existing contents of destDir are removed first so
// stale files from previous installs are cleaned out. The destination's
// parent directory is created if needed.
func (c *Component) extractRepo(bundleRelPath, destDir string) error {
	srcDir := filepath.Join(c.extractDir, bundleRelPath)
	if _, err := os.Stat(srcDir); err != nil {
		return fmt.Errorf("source %s not present in bundle: %w", bundleRelPath, err)
	}

	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("removing existing %s: %w", destDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("creating parent of %s: %w", destDir, err)
	}

	if err := copyDir(srcDir, destDir); err != nil {
		return fmt.Errorf("copying %s → %s: %w", bundleRelPath, destDir, err)
	}

	log.Printf("  installed %s", destDir)
	return nil
}

// composeVersion builds a deterministic version string that captures the
// identity of every bundled repo. Any change in any component's
// ResolvedSHA produces a new composite and forces a re-apply.
func composeVersion(m *bundle.Manifest) string {
	if m == nil {
		return ""
	}
	if m.Components.Onramp == nil && len(m.Components.HelmCharts) == 0 {
		return ""
	}

	parts := ""
	if m.Components.Onramp != nil {
		parts = "onramp:" + m.Components.Onramp.ResolvedSHA
	}
	for _, hc := range m.Components.HelmCharts {
		if parts != "" {
			parts += ","
		}
		parts += hc.Name + ":" + hc.ResolvedSHA
	}
	return parts
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// copyDir recursively copies src into dst, preserving file modes for
// regular files. Symlinks and special files are skipped — the bundled
// tree is a clean checkout so these do not appear in practice.
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

// chownTree recursively changes ownership of root to the given user and
// group. If root does not exist the call is a no-op. Missing users are a
// fatal error because that means the service_account component hasn't
// run yet, which is an ordering bug the caller should know about.
func chownTree(root, username, groupname string) error {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}

	u, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("lookup user %q: %w", username, err)
	}
	g, err := user.LookupGroup(groupname)
	if err != nil {
		return fmt.Errorf("lookup group %q: %w", groupname, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("parse uid: %w", err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return fmt.Errorf("parse gid: %w", err)
	}

	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(path, uid, gid)
	})
}
