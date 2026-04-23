// Package serviceaccount provisions two distinct OS accounts the rest of
// the bootstrap relies on:
//
//   - aether-ops — the daemon's service account. System account, no home,
//     nologin shell, no password. Only systemd starts processes as this
//     user; it never accepts interactive logins or Ansible SSH.
//
//   - onramp user (default "aether", configurable via aether_ops.onramp_user
//     in the spec) — the identity Ansible SSHes into to run aether-onramp
//     playbooks. Login shell, home directory, password set from the
//     launcher-resolved onramp password.
//
// The two accounts were briefly unified in commit 64653cf; the split was
// restored because password-based SSH into the daemon account blurred the
// daemon/deploy-identity boundary and made the sshd and sudoers drop-ins
// harder to reason about.
package serviceaccount

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"os/user"
	"strings"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/cmdutil"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/installctx"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

// daemonAccount is the service user/group under which the aether-ops
// daemon runs. Kept as a constant because nothing should ever reconfigure
// it — rke2, helm, onramp, and the systemd unit all assume this name.
const daemonAccount = "aether-ops"

// Component provisions the daemon service account and the onramp user.
type Component struct {
	manifest *bundle.Manifest
}

func New() *Component {
	return &Component{}
}

// SetManifest allows the launcher to pass the manifest for reading
// onramp user configuration.
func (c *Component) SetManifest(m *bundle.Manifest) {
	c.manifest = m
}

func (c *Component) Name() string { return "service_account" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	return b.BundleVersion
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["service_account"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	if current == desired && desired != "" {
		return components.Plan{NoOp: true}, nil
	}

	onrampUser := "aether"
	if c.manifest != nil && c.manifest.Components.AetherOps != nil {
		if c.manifest.Components.AetherOps.OnrampUser != "" {
			onrampUser = c.manifest.Components.AetherOps.OnrampUser
		}
	}

	actions := []components.Action{
		{
			Description: fmt.Sprintf("create daemon account %s", daemonAccount),
			Fn:          createDaemonAccount,
		},
		{
			Description: fmt.Sprintf("create onramp user %s", onrampUser),
			Fn: func(ctx context.Context) error {
				password := installctx.OnrampPasswordFromContext(ctx)
				if password == "" {
					return fmt.Errorf("onramp password missing on context; launcher should resolve it before Apply")
				}
				return createOnrampUser(ctx, onrampUser, password)
			},
		},
	}

	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}

// createDaemonAccount creates the aether-ops system user and group. The
// account never accepts interactive logins: no home directory, nologin
// shell, no password. Idempotent — pre-existing group/user are left alone.
func createDaemonAccount(ctx context.Context) error {
	if _, err := user.LookupGroup(daemonAccount); err != nil {
		cmd := exec.CommandContext(ctx, "groupadd", "--system", daemonAccount)
		if output, err := cmdutil.Run(ctx, cmd); err != nil {
			return fmt.Errorf("groupadd %s: %w\n%s", daemonAccount, err, output)
		}
		log.Printf("  created group %s", daemonAccount)
	}

	if _, err := user.Lookup(daemonAccount); err == nil {
		log.Printf("  user %s already exists", daemonAccount)
		return nil
	}

	cmd := exec.CommandContext(ctx, "useradd",
		"--system",
		"--no-create-home",
		"--shell", "/usr/sbin/nologin",
		"--gid", daemonAccount,
		daemonAccount,
	)
	if output, err := cmdutil.Run(ctx, cmd); err != nil {
		return fmt.Errorf("useradd %s: %w\n%s", daemonAccount, err, output)
	}
	log.Printf("  created user %s (system, nologin)", daemonAccount)
	return nil
}

// createOnrampUser creates the interactive onramp account used as the
// Ansible SSH target. The password is set on initial creation only — an
// upgrade or repair that re-runs this action leaves an existing user's
// password alone so operators who rotated it post-install don't get
// silently overwritten.
func createOnrampUser(ctx context.Context, username, password string) error {
	if err := validateUsername(username); err != nil {
		return err
	}

	// Ensure a group matching the username exists before useradd so the
	// primary-group assignment is deterministic.
	if _, err := user.LookupGroup(username); err != nil {
		cmd := exec.CommandContext(ctx, "groupadd", username)
		if output, err := cmdutil.Run(ctx, cmd); err != nil {
			return fmt.Errorf("groupadd %s: %w\n%s", username, err, output)
		}
		log.Printf("  created group %s", username)
	}

	if _, err := user.Lookup(username); err == nil {
		log.Printf("  user %s already exists (password unchanged)", username)
		return nil
	}

	cmd := exec.CommandContext(ctx, "useradd",
		"--create-home",
		"--shell", "/bin/bash",
		"--gid", username,
		username,
	)
	if output, err := cmdutil.Run(ctx, cmd); err != nil {
		return fmt.Errorf("useradd %s: %w\n%s", username, err, output)
	}
	log.Printf("  created user %s", username)

	pw := exec.CommandContext(ctx, "chpasswd")
	pw.Stdin = strings.NewReader(username + ":" + password + "\n")
	if output, err := cmdutil.Run(ctx, pw); err != nil {
		return fmt.Errorf("chpasswd for %s: %w\n%s", username, err, output)
	}
	log.Printf("  set password for %s", username)
	return nil
}

// validateUsername enforces the conservative POSIX-portable subset of
// characters allowed in a Unix username. Reused by callers that interpolate
// the onramp user into shell commands, sudoers drop-ins, or inventory files.
//
// Rationale: the username flows from a bundle spec into exec argv, sudoers
// file contents, and hosts.ini; any validation lapse is a straight path to
// command injection or sudoers corruption. Keeping the rule strict and
// centralized means every interpolation site inherits the same check.
func validateUsername(name string) error {
	if name == "" {
		return fmt.Errorf("onramp user name is empty")
	}
	if len(name) > 32 {
		return fmt.Errorf("onramp user name %q is too long (max 32)", name)
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		case (r == '_' || r == '-') && i > 0:
		default:
			return fmt.Errorf("onramp user name %q contains invalid character %q", name, r)
		}
	}
	return nil
}
